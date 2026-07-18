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
	"crypto/sha1" // #nosec G505 -- git blob ids are protocol-mandated SHA-1; test fake fidelity, not security.
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
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

// gitBlobSHA mirrors git's blob object id (SHA-1 over "blob {len}\x00{content}")
// so the fake's tree listings carry the REAL ids a client's local GitBlobSHA diff
// computes. Kept as a local copy (not an import of the parent package) so the
// fake stays a standalone wire double.
func gitBlobSHA(content []byte) string {
	h := sha1.New() // #nosec G401 -- git protocol id, not a security control.
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(content))
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

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

	// gitSeq numbers synthetic git object ids (trees/commits) minted by the
	// stateful git-data endpoints.
	gitSeq int
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

	// --- git-data (trees API) state. Not serialized. -----------------------------
	// head is the branch-tip commit sha of the default branch ("" == unborn). The
	// head commit's tree is served LIVE from `files` (sha "tree@"+head), so seeded
	// files always appear in the next tree read without bookkeeping.
	head string
	// gitBlobs stores blobs POSTed via git/blobs, keyed by their real git blob sha.
	gitBlobs map[string][]byte
	// gitTrees stores trees POSTed via git/trees as FULL file snapshots keyed by a
	// synthetic tree sha.
	gitTrees map[string]map[string][]byte
	// gitCommits stores commits POSTed via git/commits keyed by a synthetic sha.
	gitCommits map[string]fakeGitCommit
}

// fakeGitCommit is one commit object created through POST git/commits.
type fakeGitCommit struct {
	TreeSHA string
	Parents []string
	Message string
}

// liveTreeSHA is the synthetic tree id of the CURRENT branch tip's tree (served
// live from repo.files).
func (r *fakeRepo) liveTreeSHA() string { return "tree@" + r.head }

// initGitState lazily initialises the git-data maps and a synthetic head for a
// repo seeded with commit history (a default branch).
func (r *fakeRepo) initGitState() {
	if r.gitBlobs == nil {
		r.gitBlobs = map[string][]byte{}
	}
	if r.gitTrees == nil {
		r.gitTrees = map[string]map[string][]byte{}
	}
	if r.gitCommits == nil {
		r.gitCommits = map[string]fakeGitCommit{}
	}
	if r.head == "" && r.DefaultBranch != "" {
		r.head = "commit0-" + r.FullName
	}
}

// resolveTreeSnapshot resolves a tree sha to its full file snapshot: the live
// tree serves the current files; a created tree serves its stored snapshot.
func (r *fakeRepo) resolveTreeSnapshot(sha string) (map[string][]byte, bool) {
	if r.head != "" && sha == r.liveTreeSHA() {
		return copyFiles(r.files), true
	}
	snap, ok := r.gitTrees[sha]
	if ok {
		return copyFiles(snap), true
	}
	return nil, false
}

func copyFiles(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = append([]byte(nil), v...)
	}
	return out
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

// ClearRoute removes a scripted exact (method, path) route, letting the stateful
// catalog serve the endpoint again. Used by interrupted-write tests: script a
// failure, drive the fault, clear it, drive the recovery against real state.
func (f *FakeGitHub) ClearRoute(method, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.routes, method+" "+path)
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
//
// The route table is a small ordered predicate/handler list (rather than one
// large boolean switch) so this dispatcher's own cyclomatic complexity stays
// low: each route's (method, path) match lives in its own tiny isXRoute
// predicate, and each route's logic lives in its own serveCatalogX handler.
// Order matters and mirrors the original routing exactly — in particular the
// git-data route MUST be checked before the commit-tip route below it
// ("/git/commits/{sha}" also contains "/commits/").
func (f *FakeGitHub) serveCatalog(method, path, body string) (Response, bool) {
	switch {
	case isCreateRepoRoute(method, path):
		return f.serveCatalogCreateRepo(path, body)
	case isSetTopicsRoute(method, path):
		return f.serveCatalogSetTopics(path, body)
	case isGetRepoRoute(method, path):
		return f.serveCatalogGetRepo(path)
	case isListInstallationReposRoute(method, path):
		return f.serveCatalogListRepos()
	case isListBranchesRoute(method, path):
		return f.serveCatalogListBranches(path)
	case isActionsPublicKeyRoute(method, path):
		return f.serveCatalogActionsPublicKey(path)
	case isWriteSecretRoute(method, path):
		return f.serveCatalogWriteSecret(path, body)
	case isGitDataRoute(path):
		return f.serveCatalogGitData(method, path, body)
	case isCommitTipRoute(method, path):
		return f.serveCatalogCommitTip(path)
	case isContentsRoute(path):
		return f.serveCatalogContents(method, path, body)
	}
	return Response{}, false
}

// --- route predicates ---------------------------------------------------

func isCreateRepoRoute(method, path string) bool {
	return method == http.MethodPost && strings.HasPrefix(path, "/orgs/") && strings.HasSuffix(path, "/repos")
}

func isSetTopicsRoute(method, path string) bool {
	return method == http.MethodPut && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/topics")
}

func isGetRepoRoute(method, path string) bool {
	return method == http.MethodGet && strings.HasPrefix(path, "/repos/") &&
		strings.Count(strings.TrimPrefix(path, "/repos/"), "/") == 1
}

func isListInstallationReposRoute(method, path string) bool {
	return method == http.MethodGet && path == "/installation/repositories"
}

func isListBranchesRoute(method, path string) bool {
	return method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.HasSuffix(path, "/branches")
}

func isActionsPublicKeyRoute(method, path string) bool {
	return method == http.MethodGet && strings.HasPrefix(path, "/repos/") &&
		strings.HasSuffix(path, "/actions/secrets/public-key")
}

func isWriteSecretRoute(method, path string) bool {
	return method == http.MethodPut && strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/actions/secrets/")
}

func isGitDataRoute(path string) bool {
	return strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/git/")
}

func isCommitTipRoute(method, path string) bool {
	return method == http.MethodGet && strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/commits/")
}

func isContentsRoute(path string) bool {
	return strings.HasPrefix(path, "/repos/") && strings.Contains(path, "/contents/")
}

// --- route handlers -------------------------------------------------------

// serveCatalogCreateRepo handles POST /orgs/{org}/repos.
func (f *FakeGitHub) serveCatalogCreateRepo(path, body string) (Response, bool) {
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
}

// serveCatalogSetTopics handles PUT /repos/{owner}/{name}/topics.
func (f *FakeGitHub) serveCatalogSetTopics(path, body string) (Response, bool) {
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
}

// serveCatalogGetRepo handles GET /repos/{owner}/{name}.
func (f *FakeGitHub) serveCatalogGetRepo(path string) (Response, bool) {
	full := strings.TrimPrefix(path, "/repos/")
	repo, exists := f.catalog[full]
	if !exists {
		return Response{Status: 404, Body: `{"message":"not found"}`}, true
	}
	return JSON(200, repo), true
}

// serveCatalogListRepos handles GET /installation/repositories.
func (f *FakeGitHub) serveCatalogListRepos() (Response, bool) {
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
}

// serveCatalogListBranches handles GET /repos/{owner}/{name}/branches.
func (f *FakeGitHub) serveCatalogListBranches(path string) (Response, bool) {
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
}

// serveCatalogActionsPublicKey handles
// GET /repos/{owner}/{name}/actions/secrets/public-key.
func (f *FakeGitHub) serveCatalogActionsPublicKey(path string) (Response, bool) {
	full := strings.TrimSuffix(strings.TrimPrefix(path, "/repos/"), "/actions/secrets/public-key")
	if _, exists := f.catalog[full]; !exists {
		return Response{Status: 404, Body: `{"message":"not found"}`}, true
	}
	return JSON(200, map[string]any{"key_id": f.actionsKeyID, "key": f.actionsPubKeyB64}), true
}

// serveCatalogWriteSecret handles
// PUT /repos/{owner}/{name}/actions/secrets/{secretName}.
func (f *FakeGitHub) serveCatalogWriteSecret(path, body string) (Response, bool) {
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
}

// serveCatalogGitData handles /repos/{owner}/{name}/git/... (any method) by
// resolving the owning repo and delegating to serveGitData.
func (f *FakeGitHub) serveCatalogGitData(method, path, body string) (Response, bool) {
	idx := strings.Index(path, "/git/")
	full := strings.TrimPrefix(path[:idx], "/repos/")
	repo, exists := f.catalog[full]
	if !exists {
		return Response{Status: 404, Body: `{"message":"not found"}`}, true
	}
	repo.initGitState()
	return f.serveGitData(repo, method, path[idx+len("/git/"):], body)
}

// serveCatalogCommitTip handles GET /repos/{owner}/{name}/commits/{ref} (the
// default-branch tip commit).
func (f *FakeGitHub) serveCatalogCommitTip(path string) (Response, bool) {
	rest := strings.TrimPrefix(path, "/repos/")
	idx := strings.Index(rest, "/commits/")
	full := rest[:idx]
	if _, exists := f.catalog[full]; !exists {
		return Response{Status: 404, Body: `{"message":"not found"}`}, true
	}
	return JSON(200, map[string]any{"sha": "tipsha-" + full}), true
}

// serveCatalogContents handles the Contents API:
// GET|PUT /repos/{owner}/{name}/contents/{path...}.
func (f *FakeGitHub) serveCatalogContents(method, path, body string) (Response, bool) {
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
		return serveCatalogContentsGet(repo, filePath)
	case http.MethodPut:
		return serveCatalogContentsPut(repo, filePath, body)
	}
	return Response{Status: 405, Body: `{"message":"method not allowed"}`}, true
}

// serveCatalogContentsGet handles GET .../contents/{path...}.
func serveCatalogContentsGet(repo *fakeRepo, filePath string) (Response, bool) {
	raw, ok := repo.files[filePath]
	if !ok {
		return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
	}
	return JSON(200, map[string]any{
		"sha":     "blobsha-" + filePath,
		"content": base64.StdEncoding.EncodeToString(raw),
	}), true
}

// serveCatalogContentsPut handles PUT .../contents/{path...}.
func serveCatalogContentsPut(repo *fakeRepo, filePath, body string) (Response, bool) {
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

// serveGitData handles the stateful git-data (trees API) endpoints for one repo.
// `sub` is the path after "/repos/{full}/git/". Caller holds f.mu. The fake
// mirrors real GitHub semantics faithfully enough to prove the atomic-commit
// chain: blobs/trees/commits are staged invisibly, ONLY the ref update (PATCH
// refs / POST refs) makes them reachable (materialising repo.files), and an
// unforced non-fast-forward ref update is rejected 422.
//
// Dispatch is two levels (by method, then by sub-path) so each level's own
// cyclomatic complexity stays low, matching serveCatalog's pattern.
func (f *FakeGitHub) serveGitData(repo *fakeRepo, method, sub, body string) (Response, bool) {
	switch method {
	case http.MethodGet:
		return f.serveGitDataGet(repo, sub)
	case http.MethodPost:
		return f.serveGitDataPost(repo, sub, body)
	case http.MethodPatch:
		return f.serveGitDataPatch(repo, sub, body)
	}
	return Response{}, false
}

// serveGitDataGet dispatches the GET git-data sub-routes: read a tree, read a
// commit, or read a ref.
func (f *FakeGitHub) serveGitDataGet(repo *fakeRepo, sub string) (Response, bool) {
	switch {
	// Read a tree (recursive listing served either way): GET git/trees/{ref}
	case strings.HasPrefix(sub, "trees/"):
		return f.serveGitTreeGet(repo, strings.TrimPrefix(sub, "trees/"))
	// Read a commit: GET git/commits/{sha}
	case strings.HasPrefix(sub, "commits/"):
		return serveGitCommitGet(repo, strings.TrimPrefix(sub, "commits/"))
	// Read a ref: GET git/ref/heads/{branch}
	case strings.HasPrefix(sub, "ref/heads/"):
		return serveGitRefGet(repo, strings.TrimPrefix(sub, "ref/heads/"))
	}
	return Response{}, false
}

// serveGitCommitGet serves GET git/commits/{sha}: a created commit reads back
// its tree; the seeded synthetic head reads back the live tree.
func serveGitCommitGet(repo *fakeRepo, sha string) (Response, bool) {
	if c, ok := repo.gitCommits[sha]; ok {
		return JSON(200, map[string]any{"sha": sha, "tree": map[string]any{"sha": c.TreeSHA}}), true
	}
	if repo.head != "" && sha == repo.head {
		// The seeded synthetic head: its tree is the live file set.
		return JSON(200, map[string]any{"sha": sha, "tree": map[string]any{"sha": repo.liveTreeSHA()}}), true
	}
	return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
}

// serveGitRefGet serves GET git/ref/heads/{branch}.
func serveGitRefGet(repo *fakeRepo, branch string) (Response, bool) {
	if repo.head == "" || branch != repo.DefaultBranch {
		return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
	}
	return JSON(200, map[string]any{
		"ref": "refs/heads/" + branch, "object": map[string]any{"sha": repo.head, "type": "commit"},
	}), true
}

// serveGitDataPost dispatches the POST git-data sub-routes: create a blob,
// tree, commit, or ref.
func (f *FakeGitHub) serveGitDataPost(repo *fakeRepo, sub, body string) (Response, bool) {
	switch sub {
	// Create a blob: POST git/blobs
	case "blobs":
		return serveGitBlobPost(repo, body)
	// Create a tree: POST git/trees (base_tree + entries → new full snapshot)
	case "trees":
		return f.serveGitTreePost(repo, body)
	// Create a commit: POST git/commits
	case "commits":
		return f.serveGitCommitPost(repo, body)
	// Create a ref: POST git/refs
	case "refs":
		return f.serveGitRefPost(repo, body)
	}
	return Response{}, false
}

// serveGitBlobPost serves POST git/blobs.
func serveGitBlobPost(repo *fakeRepo, body string) (Response, bool) {
	var in struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	_ = json.Unmarshal([]byte(body), &in)
	raw := []byte(in.Content)
	if in.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(in.Content)
		if err != nil {
			return Response{Status: 422, Body: `{"message":"bad blob encoding"}`}, true
		}
		raw = decoded
	}
	sha := gitBlobSHA(raw)
	repo.gitBlobs[sha] = raw
	return JSON(201, map[string]any{"sha": sha}), true
}

// serveGitCommitPost serves POST git/commits.
func (f *FakeGitHub) serveGitCommitPost(repo *fakeRepo, body string) (Response, bool) {
	var in struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}
	_ = json.Unmarshal([]byte(body), &in)
	if _, ok := repo.resolveTreeSnapshot(in.Tree); !ok {
		return Response{Status: 422, Body: `{"message":"Tree SHA does not exist"}`}, true
	}
	f.gitSeq++
	sha := fmt.Sprintf("fakecommit-%d", f.gitSeq)
	repo.gitCommits[sha] = fakeGitCommit{TreeSHA: in.Tree, Parents: append([]string(nil), in.Parents...), Message: in.Message}
	return JSON(201, map[string]any{"sha": sha}), true
}

// serveGitDataPatch dispatches the PATCH git-data sub-routes: fast-forward a
// ref.
func (f *FakeGitHub) serveGitDataPatch(repo *fakeRepo, sub, body string) (Response, bool) {
	// Fast-forward a ref: PATCH git/refs/heads/{branch}
	if strings.HasPrefix(sub, "refs/heads/") {
		return f.serveGitRefPatch(repo, strings.TrimPrefix(sub, "refs/heads/"), body)
	}
	return Response{}, false
}

// serveGitTreeGet serves GET git/trees/{ref}: the branch name / head commit /
// live tree sha resolve to the CURRENT file set (with real git blob shas); a
// created tree or commit sha resolves to its stored snapshot.
func (f *FakeGitHub) serveGitTreeGet(repo *fakeRepo, ref string) (Response, bool) {
	var snap map[string][]byte
	treeSHA := ref
	switch {
	case repo.head != "" && (ref == repo.DefaultBranch || ref == repo.head || ref == repo.liveTreeSHA()):
		snap = repo.files
		treeSHA = repo.liveTreeSHA()
	default:
		if s, ok := repo.gitTrees[ref]; ok {
			snap = s
		} else if c, ok := repo.gitCommits[ref]; ok {
			s2, ok2 := repo.resolveTreeSnapshot(c.TreeSHA)
			if !ok2 {
				return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
			}
			snap, treeSHA = s2, c.TreeSHA
		} else {
			return Response{Status: 404, Body: `{"message":"Not Found"}`}, true
		}
	}
	paths := make([]string, 0, len(snap))
	for p := range snap {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	entries := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, map[string]any{
			"path": p, "mode": "100644", "type": "blob",
			"sha": gitBlobSHA(snap[p]), "size": len(snap[p]),
		})
	}
	return JSON(200, map[string]any{"sha": treeSHA, "tree": entries, "truncated": false}), true
}

// serveGitTreePost serves POST git/trees: overlay the entries (each resolving a
// previously created blob) onto the base tree's snapshot.
func (f *FakeGitHub) serveGitTreePost(repo *fakeRepo, body string) (Response, bool) {
	var in struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct {
			Path string `json:"path"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	_ = json.Unmarshal([]byte(body), &in)
	snap := map[string][]byte{}
	if in.BaseTree != "" {
		base, ok := repo.resolveTreeSnapshot(in.BaseTree)
		if !ok {
			return Response{Status: 422, Body: `{"message":"base_tree does not exist"}`}, true
		}
		snap = base
	}
	for _, e := range in.Tree {
		raw, ok := repo.gitBlobs[e.SHA]
		if !ok {
			return Response{Status: 422, Body: fmt.Sprintf(`{"message":"blob %s does not exist"}`, e.SHA)}, true
		}
		snap[e.Path] = append([]byte(nil), raw...)
	}
	f.gitSeq++
	sha := fmt.Sprintf("faketree-%d", f.gitSeq)
	repo.gitTrees[sha] = snap
	return JSON(201, map[string]any{"sha": sha}), true
}

// serveGitRefPatch serves PATCH git/refs/heads/{branch}: an UNFORCED update must
// be a fast-forward (the new commit's parent IS the current head) or it is
// rejected 422 — the compare-and-swap the atomic commit chain relies on. On
// success the commit's tree snapshot becomes the live file set (the commit is
// now reachable).
func (f *FakeGitHub) serveGitRefPatch(repo *fakeRepo, branch, body string) (Response, bool) {
	var in struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}
	_ = json.Unmarshal([]byte(body), &in)
	if repo.head == "" || branch != repo.DefaultBranch {
		return Response{Status: 422, Body: `{"message":"Reference does not exist"}`}, true
	}
	commit, ok := repo.gitCommits[in.SHA]
	if !ok {
		return Response{Status: 422, Body: `{"message":"Object does not exist"}`}, true
	}
	fastForward := len(commit.Parents) > 0 && commit.Parents[0] == repo.head
	if !fastForward && !in.Force {
		return Response{Status: 422, Body: `{"message":"Update is not a fast forward"}`}, true
	}
	snap, ok := repo.resolveTreeSnapshot(commit.TreeSHA)
	if !ok {
		return Response{Status: 422, Body: `{"message":"commit tree does not exist"}`}, true
	}
	repo.head = in.SHA
	repo.files = snap
	return JSON(200, map[string]any{
		"ref": "refs/heads/" + branch, "object": map[string]any{"sha": in.SHA, "type": "commit"},
	}), true
}

// serveGitRefPost serves POST git/refs (create a ref). Creating the ref of an
// UNBORN default branch with a staged commit materialises that commit (the
// atomic chain's fresh-repo tail); creating any other branch just records the
// name (the PR rail's CreateBranch). An existing ref is rejected 422.
func (f *FakeGitHub) serveGitRefPost(repo *fakeRepo, body string) (Response, bool) {
	var in struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	_ = json.Unmarshal([]byte(body), &in)
	branch := strings.TrimPrefix(in.Ref, "refs/heads/")
	for _, b := range repo.branches {
		if b == branch {
			return Response{Status: 422, Body: `{"message":"Reference already exists"}`}, true
		}
	}
	if commit, ok := repo.gitCommits[in.SHA]; ok && repo.head == "" {
		snap, ok2 := repo.resolveTreeSnapshot(commit.TreeSHA)
		if !ok2 {
			return Response{Status: 422, Body: `{"message":"commit tree does not exist"}`}, true
		}
		repo.head = in.SHA
		repo.files = snap
		if repo.DefaultBranch == "" {
			repo.DefaultBranch = branch
		}
	}
	repo.branches = append(repo.branches, branch)
	return JSON(201, map[string]any{
		"ref": in.Ref, "object": map[string]any{"sha": in.SHA, "type": "commit"},
	}), true
}
