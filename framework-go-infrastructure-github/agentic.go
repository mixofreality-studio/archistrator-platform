package github

// agentic.go carries the GITHUB wire plumbing the sourceControlAccess
// ResourceAccess (C-SC-AG) needs to ESTABLISH AGENTIC-DESIGN STANDING in the
// user's adopted repo: the workflow-file seat (commit the claude-code-action
// design workflow under .github/workflows/ via the Contents API) and the
// adopt-side emptiness probe (repo metadata + branch list + a shallow
// root/.aiarch contents probe) that backs adoptProjectRepo's strict-adopt
// decision (sourceControlAccess.md §2.2/§6).
//
// (2026-06-15 correction: the Actions-secret seat was REMOVED. The user's
// CLAUDE_CODE_OAUTH_TOKEN is provisioned by the Claude Code GitHub App when the
// USER runs /install-github-app on their repo — an OAuth-flow Actions secret,
// not an API-uploadable value — so aiarch does NO secret management. The
// libsodium sealed-box primitive that backed it lived only here and had only
// the now-removed RA verb as a caller, so it is deleted with no dormant caller
// left behind.)
//
// It is the satellite home for these two operation groups — kept HERE (the
// github satellite), out of the product RA, exactly as the App-JWT /
// installation-token / PR-rail / git-data / Actions-dispatch wire code is, so the
// RA stays provider-opaque.
//
// PROVIDER-OPACITY: the value types this file exposes (RepoMetadata, the
// contents methods) carry GitHub-Contents vocabulary BY DESIGN — they are the
// satellite's provider-specific surface. The consuming RA (sourceControlAccess)
// wraps them in provider-neutral WorkflowFile / RepoRef / CommitRef value types
// and never lets a GitHub lexeme cross its own contract. This file speaks GitHub;
// the RA above it does not.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// Workflow-file seat (commitAgenticWorkflowFile back-end) — Contents API.
// ---------------------------------------------------------------------------

// contentsGetDTO is the bit of GET .../contents/{path} the overwrite-if-changed
// path reads: the existing blob sha (needed on the update PUT) and the base64
// file content (to short-circuit a byte-identical re-commit).
type contentsGetDTO struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// contentsPutResultDTO is the bit of the PUT .../contents/{path} response the
// caller needs: the resulting commit sha (the opaque CommitRef).
type contentsPutResultDTO struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// PutRepoContentsFile creates-or-updates the file at `path` on the default branch
// of `fullName` with `content`, committing with `message`, via the Contents API.
// It is OVERWRITE-IF-CHANGED:
//
//   - GET .../contents/{path} to read the existing blob sha + content.
//   - If the existing content is byte-identical, NO PUT is issued — it returns the
//     existing tip's commit sha (no empty commit) and changed=false.
//   - Otherwise PUT with the existing sha (update) or without (create) and return
//     the new commit sha + changed=true.
//
// A concurrent-write race (GitHub 409 / sha mismatch on the PUT) surfaces as
// fwra.Conflict — the RA marks that retryable (retry-by-re-read). Other non-2xx
// map via ClassifyStatus (401/403 → Auth: missing contents:write).
func (c *AppClient) PutRepoContentsFile(ctx context.Context, fullName, path string, content []byte, message, instToken string) (commitSHA string, changed bool, err error) {
	if strings.TrimSpace(fullName) == "" {
		return "", false, fwra.New(fwra.ContractMisuse, "PutRepoContentsFile: empty repo")
	}
	if strings.TrimSpace(path) == "" {
		return "", false, fwra.New(fwra.ContractMisuse, "PutRepoContentsFile: empty path")
	}

	existingSHA, existingContent, found, gErr := c.getRepoContentsFile(ctx, fullName, path, instToken)
	if gErr != nil {
		return "", false, gErr
	}
	if found && bytesEqual(existingContent, content) {
		// Byte-identical — converged. Resolve the current default-branch tip as the
		// commit address without writing an empty commit.
		tip, tErr := c.getDefaultBranchTip(ctx, fullName, instToken)
		if tErr != nil {
			return "", false, tErr
		}
		return tip, false, nil
	}

	commitRef, putErr := c.sendContentsFilePut(ctx, fullName, path, existingSHA, content, message, instToken)
	if putErr != nil {
		return "", false, putErr
	}
	return commitRef, true, nil
}

func (c *AppClient) sendContentsFilePut(ctx context.Context, fullName, path, existingSHA string, content []byte, message, instToken string) (string, error) {
	payload := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
	}
	if existingSHA != "" {
		payload["sha"] = existingSHA
	}
	pb, mErr := json.Marshal(payload)
	if mErr != nil {
		return "", fwra.Wrap(fwra.ContractMisuse, mErr, "PutRepoContentsFile: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, fullName, path)
	status, respBody, dErr := c.do(ctx, http.MethodPut, url, pb, "", instToken)
	if dErr != nil {
		return "", dErr
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "PutRepoContentsFile")
	}
	var res contentsPutResultDTO
	if uerr := json.Unmarshal(respBody, &res); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "PutRepoContentsFile: decode")
	}
	return res.Commit.SHA, nil
}

// getRepoContentsFile reads the existing blob sha + decoded content at `path`. A
// 404 (absent file) is NOT an error — it returns found=false (the create path). A
// non-404 non-2xx maps via ClassifyStatus.
func (c *AppClient) getRepoContentsFile(ctx context.Context, fullName, path, instToken string) (sha string, content []byte, found bool, err error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, fullName, path)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return "", nil, false, dErr
	}
	if status == http.StatusNotFound {
		return "", nil, false, nil
	}
	if status < 200 || status >= 300 {
		return "", nil, false, ClassifyStatus(status, "getRepoContentsFile")
	}
	var dto contentsGetDTO
	if uerr := json.Unmarshal(body, &dto); uerr != nil {
		return "", nil, false, fwra.Wrap(fwra.Infrastructure, uerr, "getRepoContentsFile: decode")
	}
	// The Contents API returns base64 with embedded newlines; strip them before decode.
	decoded, derr := base64.StdEncoding.DecodeString(stripNewlines(dto.Content))
	if derr != nil {
		// A non-base64 body (e.g. a directory listing) — treat as "present but not a
		// plain file we can compare"; force a PUT path by returning empty content.
		return dto.SHA, nil, true, nil
	}
	return dto.SHA, decoded, true, nil
}

// getDefaultBranchTip resolves the current default-branch HEAD commit sha (used as
// the no-op commit address when a contents PUT is skipped as byte-identical).
func (c *AppClient) getDefaultBranchTip(ctx context.Context, fullName, instToken string) (string, error) {
	meta, err := c.GetRepoMetadata(ctx, fullName, instToken)
	if err != nil {
		return "", err
	}
	branch := meta.DefaultBranch
	if strings.TrimSpace(branch) == "" {
		branch = "main"
	}
	url := fmt.Sprintf("%s/repos/%s/commits/%s", c.baseURL, fullName, branch)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return "", dErr
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "getDefaultBranchTip")
	}
	var dto struct {
		SHA string `json:"sha"`
	}
	if uerr := json.Unmarshal(body, &dto); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "getDefaultBranchTip: decode")
	}
	return dto.SHA, nil
}

// ---------------------------------------------------------------------------
// Adopt-side emptiness probe (adoptProjectRepo back-end).
// ---------------------------------------------------------------------------

// RepoMetadata is the satellite's view of the repo-metadata bits adoptProjectRepo
// reads to decide reachability + emptiness: the full name (presence under the
// installation), the default branch name, and the cheap "has it ever been pushed"
// signals. It is GitHub-flavoured; the RA maps it to its strict-adopt decision and
// never surfaces it.
type RepoMetadata struct {
	// FullName is owner/repo as GitHub reports it (canonical address).
	FullName string
	// DefaultBranch is the repo's default branch ("main"); empty when the repo has
	// no commits yet (unborn default branch — a strong emptiness signal).
	DefaultBranch string
	// Size is the repo size in KB; 0 on a freshly-created empty repo.
	Size int
	// Topics are the repo's topics (GET /repos/{full} returns them with the github+json
	// Accept). The RA reads them to decide "already adopted by us" (aiarch-project present).
	Topics []string
}

type repoMetadataDTO struct {
	FullName      string   `json:"full_name"`
	DefaultBranch string   `json:"default_branch"`
	Size          int      `json:"size"`
	Topics        []string `json:"topics"`
}

// GetRepoMetadata fetches GET /repos/{fullName} and returns the emptiness-relevant
// metadata. A 404 surfaces as fwra.NotFound (the RA maps it to NotUnderInstallation
// when the repo is not in the installation's set / unreachable by the token).
func (c *AppClient) GetRepoMetadata(ctx context.Context, fullName, instToken string) (RepoMetadata, error) {
	url := fmt.Sprintf("%s/repos/%s", c.baseURL, fullName)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return RepoMetadata{}, dErr
	}
	if status < 200 || status >= 300 {
		return RepoMetadata{}, ClassifyStatus(status, "GetRepoMetadata")
	}
	var dto repoMetadataDTO
	if uerr := json.Unmarshal(body, &dto); uerr != nil {
		return RepoMetadata{}, fwra.Wrap(fwra.Infrastructure, uerr, "GetRepoMetadata: decode")
	}
	return RepoMetadata(dto), nil
}

// ListRepoBranches lists the repo's branch names (GET /repos/{fullName}/branches).
// A repo with ZERO branches is empty (no commit history); any branch beyond the
// default-with-only-our-init is foreign content. Used as the corroborating
// emptiness signal alongside GetRepoMetadata. A 404 (no commits → unborn) is NOT an
// error here; it returns an empty list (the repo has no branches yet).
func (c *AppClient) ListRepoBranches(ctx context.Context, fullName, instToken string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/branches?per_page=100", c.baseURL, fullName)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return nil, dErr
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status < 200 || status >= 300 {
		return nil, ClassifyStatus(status, "ListRepoBranches")
	}
	var dtos []struct {
		Name string `json:"name"`
	}
	if uerr := json.Unmarshal(body, &dtos); uerr != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, uerr, "ListRepoBranches: decode")
	}
	out := make([]string, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, d.Name)
	}
	return out, nil
}

// ProbeRepoPathExists reports whether `path` (e.g. ".aiarch") exists in the repo
// root tree via GET /repos/{fullName}/contents/{path}. A 404 means absent (NOT an
// error). A 2xx means present (any foreign tree at that path). Used by the
// strict-adopt emptiness probe to reject a repo that already carries a foreign
// .aiarch tree. A non-404 non-2xx maps via ClassifyStatus.
func (c *AppClient) ProbeRepoPathExists(ctx context.Context, fullName, path, instToken string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, fullName, path)
	status, _, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return false, dErr
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status >= 200 && status < 300 {
		return true, nil
	}
	return false, ClassifyStatus(status, "ProbeRepoPathExists")
}

// ---------------------------------------------------------------------------
// Small helpers (kept local to this file).
// ---------------------------------------------------------------------------

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stripNewlines(s string) string {
	return strings.NewReplacer("\n", "", "\r", "").Replace(s)
}
