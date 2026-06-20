// Package testinfra provides a throwaway in-process FAKE GitHub (an
// httptest.Server serving canned GitHub REST + App-auth responses) for the
// integration tests of any GitHub-backed aiarch ResourceAccess. It is TEST-ONLY:
// nothing here is imported by production code.
//
// Unlike the Gitea testinfra (a real testcontainer), GitHub has no local
// container, so the faithful test boundary is a wire-level fake of the GitHub
// REST API. The fake records every request it receives and serves scripted
// responses, so a test can assert wire-level behaviour (the right method/path,
// the App-JWT vs installation-token Authorization header, the request body) and
// drive every error-kind mapping by scripting status codes.
package testinfra

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/nacl/box"
)

// GenerateAppKeyPEM returns a freshly generated 2048-bit RSA private key in
// PKCS#1 PEM form, suitable as the GitHub App private key in tests. The App JWT
// the client mints is signed with it; the fake does not verify the signature
// (it only asserts an App-JWT-shaped Bearer header is present), so any valid RSA
// key works.
func GenerateAppKeyPEM() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block)), nil
}

// RecordedRequest captures one request the fake received.
type RecordedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string // the Authorization header value
	Body   string
}

// Response is a scripted reply for a (method, path-prefix) the fake will serve.
type Response struct {
	Status int
	Body   string
}

// FakeGitHub is the in-process fake. Configure scripted responses by exact path
// or by path prefix; unmatched routes 404. It records every request for
// wire-level assertions.
type FakeGitHub struct {
	server *httptest.Server

	mu       sync.Mutex
	routes   map[string]Response // key: "METHOD PATH" exact match
	prefixes []prefixRoute       // ordered prefix matches (checked after exact)
	requests []RecordedRequest

	// catalog is the opt-in STATEFUL repo store (EnableRepoCatalog). When non-nil the
	// fake itself serves the create/topics/get/list endpoints from in-memory state
	// faithfully: a repo created via POST /orgs/{org}/repos appears in the very next
	// GET /installation/repositories (read-after-write consistency the discover-by-
	// enumeration catalog relies on), carrying the description + topics it was given.
	// Scripted routes (On/OnPrefix) still take precedence, so error-path tests can
	// override any endpoint. nil == legacy purely-scripted fake (unchanged behaviour).
	catalog map[string]*fakeRepo // key: full name "owner/name"

	// actionsKeyID / actionsPubKeyB64 are the (fixed-per-fake) Actions encryption
	// public key the stateful catalog serves from GET .../actions/secrets/public-key.
	// A real Curve25519 box public key so the RA's libsodium SealAnonymous succeeds
	// against it. Lazily generated when the catalog is enabled.
	actionsKeyID     string
	actionsPubKeyB64 string
}

type prefixRoute struct {
	method string
	prefix string
	resp   Response
}

// fakeRepo is one in-memory repository in the stateful catalog.
type fakeRepo struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
	Private     bool     `json:"private"`
	// DefaultBranch is the repo's default branch ("main"); empty == unborn (no
	// commits) — the strong emptiness signal adoptProjectRepo reads.
	DefaultBranch string `json:"default_branch"`
	// branches is the in-memory branch set (names). An empty set == no commit
	// history (empty repo). Not serialized on the repo object.
	branches []string
	// secrets is the in-memory Actions-secret store keyed by name; the value is the
	// base64 sealed ciphertext the seal+PUT wrote (the fake stores only the sealed
	// value, never plaintext — exactly like real GitHub). Not serialized.
	secrets map[string]string
	// files is the in-memory Contents store keyed by repo path; the value is the
	// RAW (un-encoded) file bytes the PUT decoded. Not serialized.
	files map[string][]byte
}

// Start spins up the fake and returns it. Call Close (or t.Cleanup) to stop it.
func Start() *FakeGitHub {
	f := &FakeGitHub{routes: map[string]Response{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// BaseURL is the fake's REST root — pass it as apiBaseURL to NewAppClient.
func (f *FakeGitHub) BaseURL() string { return f.server.URL }

// Close stops the fake.
func (f *FakeGitHub) Close() { f.server.Close() }

// On scripts an EXACT (method, path) → response.
func (f *FakeGitHub) On(method, path string, resp Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = resp
}

// OnPrefix scripts a (method, path-prefix) → response (checked after exact
// matches, in registration order). Useful for parameterised paths like
// "/repos/acme/proj/pulls/7".
func (f *FakeGitHub) OnPrefix(method, prefix string, resp Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefixes = append(f.prefixes, prefixRoute{method: method, prefix: prefix, resp: resp})
}

// Requests returns a copy of every request received so far (for assertions).
func (f *FakeGitHub) Requests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// LastRequest returns the most recent request (or false if none).
func (f *FakeGitHub) LastRequest() (RecordedRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return RecordedRequest{}, false
	}
	return f.requests[len(f.requests)-1], true
}

// EnableRepoCatalog turns on the STATEFUL repo store: the fake then serves
// POST /orgs/{org}/repos, PUT /repos/{owner}/{name}/topics, GET /repos/{owner}/{name},
// and GET /installation/repositories from in-memory state, so a created repo appears
// in the next list with its description + topics (faithful read-after-write). Scripted
// On/OnPrefix routes still win, so a test may still override a specific endpoint to
// drive an error path. Idempotent to enable.
func (f *FakeGitHub) EnableRepoCatalog() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCatalogLocked()
}

// ensureCatalogLocked lazily creates the catalog map AND generates the fake's
// Actions encryption public key (a real Curve25519 box public key the RA's
// libsodium SealAnonymous can seal against). Caller holds f.mu.
func (f *FakeGitHub) ensureCatalogLocked() {
	if f.catalog == nil {
		f.catalog = map[string]*fakeRepo{}
	}
	if f.actionsPubKeyB64 == "" {
		pub, _, err := box.GenerateKey(rand.Reader)
		if err == nil {
			f.actionsKeyID = "fake-key-1"
			f.actionsPubKeyB64 = base64.StdEncoding.EncodeToString(pub[:])
		}
	}
}

// SeedRepo pre-populates the stateful catalog with a repo (enabling the catalog if
// needed). Useful to set up the "list returns existing aiarch projects" case without
// driving a create first.
func (f *FakeGitHub) SeedRepo(owner, name, description string, topics []string, private bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCatalogLocked()
	full := owner + "/" + name
	// A seeded repo defaults to NON-EMPTY (has a default branch + main) so existing
	// catalog tests keep their semantics; emptiness-specific seeds use SeedEmptyRepo.
	f.catalog[full] = &fakeRepo{
		Name: name, FullName: full, Description: description,
		Topics: append([]string(nil), topics...), Private: private,
		DefaultBranch: "main", branches: []string{"main"},
		secrets: map[string]string{}, files: map[string][]byte{},
	}
}

// SeedEmptyRepo seeds an UNDER-INSTALLATION, genuinely EMPTY repo (unborn default
// branch, no branches, no .aiarch tree) — the clean adopt path's input. The repo
// appears in GET /installation/repositories and GET /repos/{full} but has no
// commits, so adoptProjectRepo's emptiness probe passes.
func (f *FakeGitHub) SeedEmptyRepo(owner, name string, private bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCatalogLocked()
	full := owner + "/" + name
	f.catalog[full] = &fakeRepo{
		Name: name, FullName: full, Private: private,
		DefaultBranch: "", branches: nil,
		secrets: map[string]string{}, files: map[string][]byte{},
	}
}

// SeedAdoptedRepo seeds a repo aiarch ALREADY adopted that still carries only its
// own init: the aiarch-project topic is present and the repo is otherwise empty
// (no foreign branches/content). This is the idempotent-re-adopt input.
func (f *FakeGitHub) SeedAdoptedRepo(owner, name, description string, private bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCatalogLocked()
	full := owner + "/" + name
	f.catalog[full] = &fakeRepo{
		Name: name, FullName: full, Description: description,
		Topics: []string{"aiarch-project"}, Private: private,
		DefaultBranch: "main", branches: []string{"main"},
		secrets: map[string]string{}, files: map[string][]byte{},
	}
}

// SeedRepoFile pre-populates a file in a seeded repo's Contents store (raw bytes).
// Used to set up the overwrite-if-changed / byte-identical commit cases.
func (f *FakeGitHub) SeedRepoFile(owner, name, path string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCatalogLocked()
	full := owner + "/" + name
	repo, ok := f.catalog[full]
	if !ok {
		return
	}
	if repo.files == nil {
		repo.files = map[string][]byte{}
	}
	repo.files[path] = append([]byte(nil), content...)
}

// RepoSecretSealed returns the base64 sealed value the fake stored for a written
// Actions secret (so a test can assert a secret WAS written and that the stored
// value is NOT the plaintext). found=false if no such secret.
func (f *FakeGitHub) RepoSecretSealed(owner, name, secretName string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	repo, ok := f.catalog[owner+"/"+name]
	if !ok || repo.secrets == nil {
		return "", false
	}
	v, ok := repo.secrets[secretName]
	return v, ok
}

// RepoFile returns the raw bytes the fake stored for a committed Contents file.
func (f *FakeGitHub) RepoFile(owner, name, path string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	repo, ok := f.catalog[owner+"/"+name]
	if !ok || repo.files == nil {
		return nil, false
	}
	v, ok := repo.files[path]
	return v, ok
}

// JSON is a convenience to build a 2xx JSON Response from a value.
func JSON(status int, v any) Response {
	b, err := json.Marshal(v)
	if err != nil {
		return Response{Status: 500, Body: fmt.Sprintf(`{"message":%q}`, err.Error())}
	}
	return Response{Status: status, Body: string(b)}
}

func (f *FakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, RecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Auth:   r.Header.Get("Authorization"),
		Body:   string(body),
	})
	resp, ok := f.routes[r.Method+" "+r.URL.Path]
	if !ok {
		for _, p := range f.prefixes {
			if p.method == r.Method && strings.HasPrefix(r.URL.Path, p.prefix) {
				resp = p.resp
				ok = true
				break
			}
		}
	}
	// Stateful catalog fallback (only when no scripted route matched and the catalog
	// is enabled). Held under the same lock so create→list is atomic.
	if !ok && f.catalog != nil {
		if cr, served := f.serveCatalog(r.Method, r.URL.Path, string(body)); served {
			resp, ok = cr, true
		}
	}
	f.mu.Unlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"fake-github: no route"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	_, _ = w.Write([]byte(resp.Body))
}

// serveCatalog handles the stateful repo endpoints from in-memory state. Caller
// holds f.mu. Returns (response, true) if it handled the route.
func (f *FakeGitHub) serveCatalog(method, path, body string) (Response, bool) {
	switch {
	// Create a repo under an org: POST /orgs/{org}/repos
	case method == http.MethodPost && strings.HasPrefix(path, "/orgs/") && strings.HasSuffix(path, "/repos"):
		org := strings.TrimSuffix(strings.TrimPrefix(path, "/orgs/"), "/repos")
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Private     bool   `json:"private"`
		}
		_ = json.Unmarshal([]byte(body), &in)
		full := org + "/" + in.Name
		if _, exists := f.catalog[full]; exists {
			return Response{Status: 422, Body: `{"message":"name already exists on this account"}`}, true
		}
		repo := &fakeRepo{
			Name: in.Name, FullName: full, Description: in.Description,
			Topics: []string{}, Private: in.Private,
			DefaultBranch: "main", branches: []string{"main"},
			secrets: map[string]string{}, files: map[string][]byte{},
		}
		f.catalog[full] = repo
		return JSON(201, repo), true

	// Set topics: PUT /repos/{owner}/{name}/topics
	case method == http.MethodPut && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/topics"):
		full := strings.TrimSuffix(strings.TrimPrefix(path, "/repos/"), "/topics")
		repo, exists := f.catalog[full]
		if !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		var in struct {
			Names []string `json:"names"`
		}
		_ = json.Unmarshal([]byte(body), &in)
		repo.Topics = append([]string(nil), in.Names...)
		return JSON(200, map[string]any{"names": repo.Topics}), true

	// Get a single repo: GET /repos/{owner}/{name}
	case method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.Count(strings.TrimPrefix(path, "/repos/"), "/") == 1:
		full := strings.TrimPrefix(path, "/repos/")
		repo, exists := f.catalog[full]
		if !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		return JSON(200, repo), true

	// List installation repos: GET /installation/repositories
	case method == http.MethodGet && path == "/installation/repositories":
		fulls := make([]string, 0, len(f.catalog))
		for k := range f.catalog {
			fulls = append(fulls, k)
		}
		sort.Strings(fulls)
		repos := make([]*fakeRepo, 0, len(fulls))
		for _, k := range fulls {
			repos = append(repos, f.catalog[k])
		}
		return JSON(200, map[string]any{"total_count": len(repos), "repositories": repos}), true

	// List branches: GET /repos/{owner}/{name}/branches
	case method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/branches"):
		full := strings.TrimSuffix(strings.TrimPrefix(path, "/repos/"), "/branches")
		repo, exists := f.catalog[full]
		if !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		out := make([]map[string]any, 0, len(repo.branches))
		for _, b := range repo.branches {
			out = append(out, map[string]any{"name": b})
		}
		return JSON(200, out), true

	// Actions secret public key: GET /repos/{owner}/{name}/actions/secrets/public-key
	case method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/actions/secrets/public-key"):
		full := strings.TrimSuffix(strings.TrimPrefix(path, "/repos/"), "/actions/secrets/public-key")
		if _, exists := f.catalog[full]; !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		return JSON(200, map[string]any{"key_id": f.actionsKeyID, "key": f.actionsPubKeyB64}), true

	// Write/upsert an Actions secret: PUT /repos/{owner}/{name}/actions/secrets/{secretName}
	case method == http.MethodPut && strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/actions/secrets/"):
		rest := strings.TrimPrefix(path, "/repos/")
		idx := strings.Index(rest, "/actions/secrets/")
		full := rest[:idx]
		secretName := rest[idx+len("/actions/secrets/"):]
		repo, exists := f.catalog[full]
		if !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		var in struct {
			EncryptedValue string `json:"encrypted_value"`
			KeyID          string `json:"key_id"`
		}
		_ = json.Unmarshal([]byte(body), &in)
		if repo.secrets == nil {
			repo.secrets = map[string]string{}
		}
		_, existed := repo.secrets[secretName]
		repo.secrets[secretName] = in.EncryptedValue // stores ONLY the sealed value
		if existed {
			return Response{Status: 204, Body: ""}, true // updated
		}
		return Response{Status: 201, Body: ""}, true // created

	// Default-branch tip commit: GET /repos/{owner}/{name}/commits/{ref}
	case method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/commits/"):
		rest := strings.TrimPrefix(path, "/repos/")
		idx := strings.Index(rest, "/commits/")
		full := rest[:idx]
		if _, exists := f.catalog[full]; !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		return JSON(200, map[string]any{"sha": "tipsha-" + full}), true

	// Contents API (GET existing file / PUT create-or-update):
	//   GET|PUT /repos/{owner}/{name}/contents/{path...}
	case strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/contents/"):
		rest := strings.TrimPrefix(path, "/repos/")
		idx := strings.Index(rest, "/contents/")
		full := rest[:idx]
		filePath := rest[idx+len("/contents/"):]
		repo, exists := f.catalog[full]
		if !exists {
			return Response{Status: 404, Body: `{"message":"not found"}`}, true
		}
		switch method {
		case http.MethodGet:
			raw, ok := repo.files[filePath]
			if !ok {
				return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
			}
			return JSON(200, map[string]any{
				"sha":     "blobsha-" + filePath,
				"content": base64.StdEncoding.EncodeToString(raw),
			}), true
		case http.MethodPut:
			var in struct {
				Message string `json:"message"`
				Content string `json:"content"`
				SHA     string `json:"sha"`
			}
			_ = json.Unmarshal([]byte(body), &in)
			decoded, derr := base64.StdEncoding.DecodeString(in.Content)
			if derr != nil {
				return Response{Status: 422, Body: `{"message":"bad content"}`}, true
			}
			if repo.files == nil {
				repo.files = map[string][]byte{}
			}
			repo.files[filePath] = decoded
			return JSON(201, map[string]any{"commit": map[string]any{"sha": "commitsha-" + filePath}}), true
		}
		return Response{Status: 405, Body: `{"message":"method not allowed"}`}, true
	}
	return Response{}, false
}
