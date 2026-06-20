// Package github is the sanctioned GitHub-App infrastructure toolkit for systems
// built with aiarch. It carries the generic, reusable GitHub-App wire plumbing
// every aiarch app's GitHub-backed ResourceAccess needs — App-JWT (RS256)
// minting, the installation-token exchange, per-repo provisioning, the
// pull-request-rail REST calls, and the mapping of a GitHub REST fault onto the
// framework error model — so each component does not re-implement it.
//
// GitHub (via a GitHub App) is one of the FIXED CustomerAppInfrastructure
// options an aiarch-built app may use (the same menu that lists self-hosted
// Gitea — see framework-go-infrastructure-gitea, and the dependency allowlist
// enforced by framework-go/arch). The companion testinfra subpackage spins a
// throwaway in-process fake-GitHub (an httptest.Server serving canned GitHub
// REST + App-auth responses) for the integration tests of any GitHub-backed RA.
//
// PROVIDER-OPACITY BOUNDARY: this satellite is the ONLY place GitHub wire
// lexemes (installation tokens, installation ids, App JWTs, owner/repo, the PR
// REST shapes) live. The consuming ResourceAccess (sourceControlAccess) wraps
// these in provider-neutral value types and never lets a GitHub lexeme cross its
// contract surface. This file speaks GitHub; the RA above it does not.
//
// STDLIB-ONLY JWT: the App JWT is an RS256-signed JWT, minted here using only the
// Go standard library (crypto/rsa, crypto/sha256, crypto/x509, encoding/base64,
// encoding/json). No third-party JWT library is pulled — that keeps the
// dependency surface tiny and keeps consuming server modules on their arch
// allowlist (which admits the daveandamira/ family but not a JWT vendor).
package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// defaultAPIBaseURL is github.com's REST root. An empty base URL (GHE / a test
// fake) is overridden by the caller; github.com is the production default.
const defaultAPIBaseURL = "https://api.github.com"

// appJWTTTL is the lifetime of the App JWT minted for App-level calls. GitHub
// caps App JWTs at 10 minutes; we use a conservative window and a small clock
// skew back-date, per GitHub's own guidance.
const (
	appJWTTTL   = 9 * time.Minute
	appJWTSkew  = 30 * time.Second
	httpTimeout = 30 * time.Second
)

// AppClient is the concrete GitHub-App REST client behind the sourceControlAccess
// seam. It holds the App identity (App ID + RSA private key) and mints App JWTs +
// installation tokens on demand. It is provider-specific by design — every method
// speaks GitHub; the RA above wraps the results in provider-neutral types.
//
// It performs no IO at construction; infrastructure faults surface lazily on the
// first call as typed fwra errors.
type AppClient struct {
	// baseURL is the REST root (github.com by default; a GHE host or a test fake
	// overrides it). No trailing slash.
	baseURL string
	// appID is the numeric GitHub App id (as a string; never parsed by callers).
	appID string
	// privateKey is the App's RSA private key used to sign the App JWT (RS256).
	privateKey *rsa.PrivateKey
	// http is the underlying HTTP client.
	http *http.Client
	// now is the clock (overridable in tests for deterministic JWT exp/iat).
	now func() time.Time
}

// NewAppClient builds the concrete GitHub-App client. appID is the numeric App
// id; privateKeyPEM is the App's RSA private key (PEM, PKCS#1 or PKCS#8);
// apiBaseURL is the REST root (empty == github.com). It validates the PEM eagerly
// (a bad key is a configuration error surfaced as fwra.ContractMisuse) but
// performs no network IO.
//
// In production the three values come from the ARCHISTRATOR_GITHUB_APP_* env via
// the composition root; this constructor stays env-detail-free.
func NewAppClient(appID, privateKeyPEM, apiBaseURL string) (*AppClient, error) {
	if strings.TrimSpace(appID) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewAppClient: empty appID")
	}
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fwra.Wrap(fwra.ContractMisuse, err, "NewAppClient: invalid private key PEM")
	}
	base := strings.TrimSpace(apiBaseURL)
	if base == "" {
		base = defaultAPIBaseURL
	}
	return &AppClient{
		baseURL:    strings.TrimRight(base, "/"),
		appID:      strings.TrimSpace(appID),
		privateKey: key,
		http:       &http.Client{Timeout: httpTimeout},
		now:        time.Now,
	}, nil
}

// parseRSAPrivateKey decodes a PEM-encoded RSA private key (PKCS#1 or PKCS#8).
func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}

// ---------------------------------------------------------------------------
// App JWT (RS256) — minted with the standard library only.
// ---------------------------------------------------------------------------

// mintAppJWT produces a short-lived RS256-signed App JWT. The App JWT
// authenticates App-LEVEL calls (installation discovery, installation-token
// exchange); it is never threaded outside this package.
func (c *AppClient) mintAppJWT() (string, error) {
	now := c.now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-appJWTSkew).Unix(),
		"exp": now.Add(appJWTTTL).Unix(),
		"iss": c.appID,
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, err, "mintAppJWT: marshal header")
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, err, "mintAppJWT: marshal claims")
	}
	signingInput := b64url(hb) + "." + b64url(cb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fwra.Wrap(fwra.Infrastructure, err, "mintAppJWT: sign")
	}
	return signingInput + "." + b64url(sig), nil
}

// b64url is RFC-7515 base64url-without-padding (the JWT encoding).
func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// ---------------------------------------------------------------------------
// Wire DTOs — package-internal JSON views of the bits of the GitHub REST API
// this client reads/writes. NONE crosses the RA contract surface.
// ---------------------------------------------------------------------------

type installationDTO struct {
	ID      int64       `json:"id"`
	Account *accountDTO `json:"account,omitempty"`
}

type accountDTO struct {
	Login string `json:"login"`
}

type tokenDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type repoDTO struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Topics      []string  `json:"topics"`
	Private     bool      `json:"private"`
	CreatedAt   time.Time `json:"created_at"`
	PushedAt    time.Time `json:"pushed_at"`
}

// installationReposDTO is the paginated envelope GitHub returns for
// GET /installation/repositories ({total_count, repositories:[...]}).
type installationReposDTO struct {
	TotalCount   int       `json:"total_count"`
	Repositories []repoDTO `json:"repositories"`
}

// RepoInfo is the provider-flavoured view of one repository the consuming RA maps
// to its own provider-neutral catalog row. It carries the bits installation-repo
// enumeration needs: the repo name/full-name, its description + topics (the aiarch
// catalog discriminators), visibility, and the create/push timestamps (cheap — both
// ride on the repos payload already). It is package-internal vocabulary that the
// sourceControlAccess wraps; it crosses NO Manager contract surface.
type RepoInfo struct {
	Name        string
	FullName    string
	Description string
	Topics      []string
	Private     bool
	CreatedAt   time.Time
	PushedAt    time.Time
}

// CreateRepoOptions carries the create-time repo metadata beyond name/visibility:
// the human-facing Description and the Topics to apply (topics are set in a separate
// PUT after create — GitHub has no create-time topics field). An options struct
// keeps CreateOrgRepo's signature from sprouting positional params as the create
// metadata grows.
type CreateRepoOptions struct {
	// Description is the repo's human-facing description (the project title, for an
	// aiarch project repo). Empty leaves it unset.
	Description string
	// Topics are applied via SetRepoTopics after the repo exists. aiarch project
	// repos carry the "aiarch-project" topic so installation-repo enumeration can
	// filter the catalog by topic.
	Topics []string
}

// ---------------------------------------------------------------------------
// App-lifecycle calls (back contract #1, ISourceControlLifecycle).
// ---------------------------------------------------------------------------

// FindInstallation discovers the installation id for `account` by listing the
// App's installations and matching the account login. A missing installation
// (the user has not installed the App) surfaces as fwra.NotFound.
func (c *AppClient) FindInstallation(ctx context.Context, account string) (int64, error) {
	jwt, err := c.mintAppJWT()
	if err != nil {
		return 0, err
	}
	status, body, err := c.do(ctx, http.MethodGet, c.baseURL+"/app/installations", nil, jwt, "")
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, ClassifyStatus(status, "FindInstallation")
	}
	var installs []installationDTO
	if uerr := json.Unmarshal(body, &installs); uerr != nil {
		return 0, fwra.Wrap(fwra.Infrastructure, uerr, "FindInstallation: decode")
	}
	for _, inst := range installs {
		if inst.Account != nil && strings.EqualFold(inst.Account.Login, account) {
			return inst.ID, nil
		}
	}
	return 0, fwra.New(fwra.NotFound, "FindInstallation: app not installed on account "+account)
}

// MintInstallationToken exchanges the App JWT for a short-lived installation
// token scoped to `installationID`. Returns the bearer token + its expiry.
func (c *AppClient) MintInstallationToken(ctx context.Context, installationID int64) (token string, expiresAt time.Time, err error) {
	jwt, mErr := c.mintAppJWT()
	if mErr != nil {
		return "", time.Time{}, mErr
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)
	status, body, dErr := c.do(ctx, http.MethodPost, url, []byte("{}"), jwt, "")
	if dErr != nil {
		return "", time.Time{}, dErr
	}
	if status < 200 || status >= 300 {
		return "", time.Time{}, ClassifyStatus(status, "MintInstallationToken")
	}
	var tok tokenDTO
	if uerr := json.Unmarshal(body, &tok); uerr != nil {
		return "", time.Time{}, fwra.Wrap(fwra.Infrastructure, uerr, "MintInstallationToken: decode")
	}
	if strings.TrimSpace(tok.Token) == "" {
		return "", time.Time{}, fwra.New(fwra.Infrastructure, "MintInstallationToken: empty token in response")
	}
	return tok.Token, tok.ExpiresAt, nil
}

// CreateOrgRepo provisions a repo named `name` under org `account` using the
// installation token, applying opts.Description at create and opts.Topics in a
// follow-up SetRepoTopics call (GitHub has no create-time topics field). A 422
// "already exists" is reported as alreadyExists==true WITHOUT error (the RA maps
// that to idempotent success) — and on that path the topics are reconciled onto the
// existing repo too, so re-provisioning a repo created before topics existed heals
// it. Returns the repo's full name (owner/repo) as the opaque address.
func (c *AppClient) CreateOrgRepo(ctx context.Context, account, name, instToken string, private bool, opts CreateRepoOptions) (fullName string, alreadyExists bool, err error) {
	payload := map[string]any{"name": name, "private": private, "auto_init": true}
	if opts.Description != "" {
		payload["description"] = opts.Description
	}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return "", false, fwra.Wrap(fwra.ContractMisuse, mErr, "CreateOrgRepo: marshal")
	}
	url := fmt.Sprintf("%s/orgs/%s/repos", c.baseURL, account)
	status, respBody, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return "", false, dErr
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
		// Already exists — fetch the existing repo and report idempotent success.
		full, fErr := c.getRepoFullName(ctx, account, name, instToken)
		if fErr != nil {
			return "", false, fErr
		}
		if len(opts.Topics) > 0 {
			if tErr := c.SetRepoTopics(ctx, full, instToken, opts.Topics); tErr != nil {
				return "", false, tErr
			}
		}
		return full, true, nil
	}
	if status < 200 || status >= 300 {
		return "", false, ClassifyStatus(status, "CreateOrgRepo")
	}
	var repo repoDTO
	if uerr := json.Unmarshal(respBody, &repo); uerr != nil {
		return "", false, fwra.Wrap(fwra.Infrastructure, uerr, "CreateOrgRepo: decode")
	}
	if len(opts.Topics) > 0 {
		if tErr := c.SetRepoTopics(ctx, repo.FullName, instToken, opts.Topics); tErr != nil {
			return "", false, tErr
		}
	}
	return repo.FullName, false, nil
}

// SetRepoTopics replaces the topic set on `fullName` (owner/repo) with `topics` via
// PUT /repos/{fullName}/topics. Idempotent (PUT replaces). The topics endpoint
// requires the default github+json Accept (already set by do).
func (c *AppClient) SetRepoTopics(ctx context.Context, fullName, instToken string, topics []string) error {
	if topics == nil {
		topics = []string{}
	}
	body, mErr := json.Marshal(map[string]any{"names": topics})
	if mErr != nil {
		return fwra.Wrap(fwra.ContractMisuse, mErr, "SetRepoTopics: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/topics", c.baseURL, fullName)
	status, _, dErr := c.do(ctx, http.MethodPut, url, body, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status < 200 || status >= 300 {
		return ClassifyStatus(status, "SetRepoTopics")
	}
	return nil
}

// ListInstallationRepos enumerates every repository the installation (the App's
// install on the account) can see, via GET /installation/repositories with
// per_page=100 pagination followed until exhausted. It uses the installation token
// directly (NOT the search API) because /installation/repositories is
// read-after-write consistent — a repo just created with the same token appears in
// the very next list, which is the property aiarch's discover-by-enumeration catalog
// depends on. The repos payload returns description + topics + visibility by default
// (github+json Accept, set by do), so no extra round-trip per repo is needed.
func (c *AppClient) ListInstallationRepos(ctx context.Context, instToken string) ([]RepoInfo, error) {
	var out []RepoInfo
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", c.baseURL, page)
		status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, ClassifyStatus(status, "ListInstallationRepos")
		}
		var env installationReposDTO
		if uerr := json.Unmarshal(body, &env); uerr != nil {
			return nil, fwra.Wrap(fwra.Infrastructure, uerr, "ListInstallationRepos: decode")
		}
		for _, r := range env.Repositories {
			out = append(out, RepoInfo{
				Name:        r.Name,
				FullName:    r.FullName,
				Description: r.Description,
				Topics:      r.Topics,
				Private:     r.Private,
				CreatedAt:   r.CreatedAt,
				PushedAt:    r.PushedAt,
			})
		}
		// Exhausted once a short page (fewer than per_page) is returned, or once the
		// accumulated count reaches the reported total_count.
		if len(env.Repositories) < 100 || (env.TotalCount > 0 && len(out) >= env.TotalCount) {
			break
		}
	}
	return out, nil
}

// getRepoFullName fetches owner/repo to confirm an existing repo on the
// already-exists path.
func (c *AppClient) getRepoFullName(ctx context.Context, account, name, instToken string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, account, name)
	status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "getRepoFullName")
	}
	var repo repoDTO
	if uerr := json.Unmarshal(body, &repo); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "getRepoFullName: decode")
	}
	return repo.FullName, nil
}

// ---------------------------------------------------------------------------
// HTTP transport + error classification (shared by all calls).
// ---------------------------------------------------------------------------

// do issues a GitHub REST request authenticated with EITHER the App JWT
// (appJWT non-empty) OR an installation/repo token (token non-empty), returning
// (status, body). Exactly one of appJWT/token should be supplied.
func (c *AppClient) do(ctx context.Context, method, url string, body []byte, appJWT, token string) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, fwra.Wrap(fwra.ContractMisuse, err, "github client: build request")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case appJWT != "":
		req.Header.Set("Authorization", "Bearer "+appJWT)
	case token != "":
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, fwra.Wrap(fwra.Transient, err, "github client: request cancelled/timed out")
		}
		return 0, nil, fwra.Wrap(fwra.Transient, err, "github client: transport error")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fwra.Wrap(fwra.Transient, err, "github client: read response body")
	}
	return resp.StatusCode, respBody, nil
}

// ClassifyStatus maps a non-2xx GitHub REST status code onto the shared
// framework error model:
//
//   - 401/403           => fwra.Auth           (terminal: App JWT rejected / installation revoked / insufficient permission)
//   - 404               => fwra.NotFound       (terminal: app not installed / unknown repo|branch|PR)
//   - 405               => fwra.Conflict       (terminal: PR not mergeable — the rail's NotMergeable case)
//   - 409               => fwra.Conflict       (terminal: merge conflict / already-exists handled by callers)
//   - 422               => fwra.ContractMisuse  (terminal: GitHub rejected the request body) — callers may intercept the already-exists subcase before this
//   - 429 / 5xx         => fwra.Transient       (retryable: rate-limit / GitHub 5xx)
//   - anything else     => fwra.Infrastructure  (escalate)
//
// The already-exists (422/409 on create) and already-merged subcases are handled
// by the calling methods (CreateOrgRepo etc.) BEFORE this classifier — they map
// to idempotent success, never to an error.
func ClassifyStatus(status int, op string) error {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fwra.New(fwra.Auth, op+": github auth/permission denied")
	case status == http.StatusNotFound:
		return fwra.New(fwra.NotFound, op+": github resource not found")
	case status == http.StatusMethodNotAllowed:
		return fwra.New(fwra.Conflict, op+": github resource not mergeable")
	case status == http.StatusConflict:
		return fwra.New(fwra.Conflict, op+": github conflict")
	case status == http.StatusUnprocessableEntity:
		return fwra.New(fwra.ContractMisuse, op+": github rejected the request")
	case status == http.StatusTooManyRequests, status >= 500:
		return fwra.New(fwra.Transient, fmt.Sprintf("%s: github transient %d", op, status))
	default:
		return fwra.New(fwra.Infrastructure, fmt.Sprintf("%s: github status %d", op, status))
	}
}
