package github

// github_pullrequest.go carries the GitHub PR-rail REST plumbing backing
// IPullRequestRail (sourceControlAccess contract #2): branch creation, pull
// request open/status/review/merge, and main-branch protection. All wire lexemes
// (refs/heads, /pulls, /merge, required_status_checks) live here; the RA above
// wraps every result in a provider-neutral value type.
//
// Every call authenticates with a caller-supplied installation token (`instToken`)
// — the credential the RA threads in from contract #1's MintInstallationToken,
// Manager-orchestrated. This satellite never mints a token for these calls; it
// is handed one.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// PR-rail wire DTOs — package-internal.
// ---------------------------------------------------------------------------

type refObjectDTO struct {
	SHA string `json:"sha"`
}

type refDTO struct {
	Ref    string       `json:"ref"`
	Object refObjectDTO `json:"object"`
}

type pullRequestDTO struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Merged    bool   `json:"merged"`
	Mergeable *bool  `json:"mergeable"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type mergeResultDTO struct {
	SHA    string `json:"sha"`
	Merged bool   `json:"merged"`
}

type checkRunsDTO struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"check_runs"`
}

type reviewDTO struct {
	State string `json:"state"`
}

// CheckRollup is the provider-neutral CI rollup the RA folds into its status type.
type CheckRollup int

const (
	// RollupPending — at least one check still running / queued, none failed.
	RollupPending CheckRollup = iota
	// RollupSuccess — all checks concluded successfully (or none present).
	RollupSuccess
	// RollupFailure — at least one check failed / errored / was cancelled.
	RollupFailure
)

// PullStatus is the satellite-level snapshot the RA maps onto its
// provider-neutral PullRequestStatus.
type PullStatus struct {
	Rollup        CheckRollup
	ApprovalCount int
	Mergeable     bool
}

// ---------------------------------------------------------------------------
// Branch.
// ---------------------------------------------------------------------------

// CreateBranch cuts `branch` from the current tip of `base` (typically main) in
// owner/repo `fullName`. A 422 (ref already exists) is reported as
// alreadyExists==true WITHOUT error (the RA maps that to idempotent success).
func (c *AppClient) CreateBranch(ctx context.Context, fullName, base, branch, instToken string) (alreadyExists bool, err error) {
	baseSHA, gErr := c.getRefSHA(ctx, fullName, "heads/"+base, instToken)
	if gErr != nil {
		return false, gErr
	}
	payload := map[string]string{"ref": "refs/heads/" + branch, "sha": baseSHA}
	body, mErr := json.Marshal(payload)
	if mErr != nil {
		return false, fwra.Wrap(fwra.ContractMisuse, mErr, "CreateBranch: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/git/refs", c.baseURL, fullName)
	status, _, dErr := c.do(ctx, http.MethodPost, url, body, "", instToken)
	if dErr != nil {
		return false, dErr
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
		return true, nil // ref already exists → idempotent success
	}
	if status < 200 || status >= 300 {
		return false, ClassifyStatus(status, "CreateBranch")
	}
	return false, nil
}

// getRefSHA reads the commit SHA a ref points at (ref e.g. "heads/main").
func (c *AppClient) getRefSHA(ctx context.Context, fullName, ref, instToken string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/git/ref/%s", c.baseURL, fullName, ref)
	status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", ClassifyStatus(status, "getRefSHA")
	}
	var r refDTO
	if uerr := json.Unmarshal(body, &r); uerr != nil {
		return "", fwra.Wrap(fwra.Infrastructure, uerr, "getRefSHA: decode")
	}
	return r.Object.SHA, nil
}

// ---------------------------------------------------------------------------
// Pull request.
// ---------------------------------------------------------------------------

// OpenPullRequest proposes head→base in owner/repo `fullName`. If an open PR for
// the head→base pair already exists, it is fetched and reported as
// alreadyExists==true WITHOUT error. Returns the PR number.
func (c *AppClient) OpenPullRequest(ctx context.Context, fullName, head, base, title, body, instToken string) (number int, alreadyExists bool, err error) {
	payload := map[string]any{"title": title, "head": head, "base": base, "body": body}
	pb, mErr := json.Marshal(payload)
	if mErr != nil {
		return 0, false, fwra.Wrap(fwra.ContractMisuse, mErr, "OpenPullRequest: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/pulls", c.baseURL, fullName)
	status, respBody, dErr := c.do(ctx, http.MethodPost, url, pb, "", instToken)
	if dErr != nil {
		return 0, false, dErr
	}
	if status == http.StatusUnprocessableEntity {
		// A PR for this head→base may already exist — look it up.
		n, found, fErr := c.findOpenPR(ctx, fullName, head, base, instToken)
		if fErr != nil {
			return 0, false, fErr
		}
		if found {
			return n, true, nil
		}
		return 0, false, ClassifyStatus(status, "OpenPullRequest")
	}
	if status < 200 || status >= 300 {
		return 0, false, ClassifyStatus(status, "OpenPullRequest")
	}
	var pr pullRequestDTO
	if uerr := json.Unmarshal(respBody, &pr); uerr != nil {
		return 0, false, fwra.Wrap(fwra.Infrastructure, uerr, "OpenPullRequest: decode")
	}
	return pr.Number, false, nil
}

// findOpenPR looks up an existing open PR for head→base (the already-exists path).
func (c *AppClient) findOpenPR(ctx context.Context, fullName, head, base, instToken string) (number int, found bool, err error) {
	// GitHub's head filter is "owner:branch"; the owner is the first segment of fullName.
	owner := fullName
	if i := strings.Index(fullName, "/"); i >= 0 {
		owner = fullName[:i]
	}
	url := fmt.Sprintf("%s/repos/%s/pulls?state=open&head=%s:%s&base=%s", c.baseURL, fullName, owner, head, base)
	status, body, dErr := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if dErr != nil {
		return 0, false, dErr
	}
	if status < 200 || status >= 300 {
		return 0, false, ClassifyStatus(status, "findOpenPR")
	}
	var prs []pullRequestDTO
	if uerr := json.Unmarshal(body, &prs); uerr != nil {
		return 0, false, fwra.Wrap(fwra.Infrastructure, uerr, "findOpenPR: decode")
	}
	if len(prs) == 0 {
		return 0, false, nil
	}
	return prs[0].Number, true, nil
}

// GetPullStatus folds the PR's mergeability, CI check rollup, and approval count
// into a provider-neutral PullStatus.
func (c *AppClient) GetPullStatus(ctx context.Context, fullName string, number int, instToken string) (PullStatus, error) {
	pr, err := c.getPullRequest(ctx, fullName, number, instToken)
	if err != nil {
		return PullStatus{}, err
	}
	rollup, err := c.getCheckRollup(ctx, fullName, pr.Head.SHA, instToken)
	if err != nil {
		return PullStatus{}, err
	}
	approvals, err := c.getApprovalCount(ctx, fullName, number, instToken)
	if err != nil {
		return PullStatus{}, err
	}
	mergeable := false
	if pr.Mergeable != nil {
		mergeable = *pr.Mergeable
	}
	return PullStatus{Rollup: rollup, ApprovalCount: approvals, Mergeable: mergeable}, nil
}

func (c *AppClient) getPullRequest(ctx context.Context, fullName string, number int, instToken string) (pullRequestDTO, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d", c.baseURL, fullName, number)
	status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if err != nil {
		return pullRequestDTO{}, err
	}
	if status < 200 || status >= 300 {
		return pullRequestDTO{}, ClassifyStatus(status, "getPullRequest")
	}
	var pr pullRequestDTO
	if uerr := json.Unmarshal(body, &pr); uerr != nil {
		return pullRequestDTO{}, fwra.Wrap(fwra.Infrastructure, uerr, "getPullRequest: decode")
	}
	return pr, nil
}

func (c *AppClient) getCheckRollup(ctx context.Context, fullName, sha, instToken string) (CheckRollup, error) {
	if strings.TrimSpace(sha) == "" {
		return RollupPending, nil
	}
	url := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs", c.baseURL, fullName, sha)
	status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if err != nil {
		return RollupPending, err
	}
	if status < 200 || status >= 300 {
		return RollupPending, ClassifyStatus(status, "getCheckRollup")
	}
	var runs checkRunsDTO
	if uerr := json.Unmarshal(body, &runs); uerr != nil {
		return RollupPending, fwra.Wrap(fwra.Infrastructure, uerr, "getCheckRollup: decode")
	}
	return foldCheckRollup(runs), nil
}

// foldCheckRollup reduces the per-check-run states into a single rollup:
// failure if any check failed; pending if any is still running; success
// otherwise (including the no-checks case — nothing is blocking).
func foldCheckRollup(runs checkRunsDTO) CheckRollup {
	pending := false
	for _, r := range runs.CheckRuns {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "success", "neutral", "skipped":
			// counts as not-blocking
		default: // failure, cancelled, timed_out, action_required, stale, ""
			return RollupFailure
		}
	}
	if pending {
		return RollupPending
	}
	return RollupSuccess
}

func (c *AppClient) getApprovalCount(ctx context.Context, fullName string, number int, instToken string) (int, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews", c.baseURL, fullName, number)
	status, body, err := c.do(ctx, http.MethodGet, url, nil, "", instToken)
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, ClassifyStatus(status, "getApprovalCount")
	}
	var reviews []reviewDTO
	if uerr := json.Unmarshal(body, &reviews); uerr != nil {
		return 0, fwra.Wrap(fwra.Infrastructure, uerr, "getApprovalCount: decode")
	}
	count := 0
	for _, r := range reviews {
		if strings.EqualFold(r.State, "APPROVED") {
			count++
		}
	}
	return count, nil
}

// PostReview records a review on the PR. `event` is the GitHub review event
// ("APPROVE" | "REQUEST_CHANGES" | "COMMENT").
func (c *AppClient) PostReview(ctx context.Context, fullName string, number int, event, body, instToken string) error {
	payload := map[string]string{"event": event, "body": body}
	pb, mErr := json.Marshal(payload)
	if mErr != nil {
		return fwra.Wrap(fwra.ContractMisuse, mErr, "PostReview: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews", c.baseURL, fullName, number)
	status, _, dErr := c.do(ctx, http.MethodPost, url, pb, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status < 200 || status >= 300 {
		return ClassifyStatus(status, "PostReview")
	}
	return nil
}

// MergePullRequest performs the merge as the authenticated identity (the App).
// already-merged is reported as alreadyMerged==true WITHOUT error; a 405 (not
// mergeable) and 409 (conflict) surface as typed errors (Conflict) the RA maps to
// NotMergeable/Conflict for the Manager to route back to interventionEngine.
func (c *AppClient) MergePullRequest(ctx context.Context, fullName string, number int, instToken string) (commit string, alreadyMerged bool, err error) {
	// If the PR is already merged, GitHub's merge PUT 405s; check first for a
	// clean idempotent-success path.
	pr, gErr := c.getPullRequest(ctx, fullName, number, instToken)
	if gErr != nil {
		return "", false, gErr
	}
	if pr.Merged {
		return "", true, nil
	}
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/merge", c.baseURL, fullName, number)
	status, respBody, dErr := c.do(ctx, http.MethodPut, url, []byte("{}"), "", instToken)
	if dErr != nil {
		return "", false, dErr
	}
	if status < 200 || status >= 300 {
		return "", false, ClassifyStatus(status, "MergePullRequest")
	}
	var res mergeResultDTO
	if uerr := json.Unmarshal(respBody, &res); uerr != nil {
		return "", false, fwra.Wrap(fwra.Infrastructure, uerr, "MergePullRequest: decode")
	}
	return res.SHA, false, nil
}

// ConfigureBranchProtection provisions main-branch protection that restricts
// merges to the App, requires status checks + ≥1 approval, and blocks out-of-band
// direct pushes — with the App itself retained as a bypass actor for aiarch's own
// gated state commits. Desired-state PUT; naturally idempotent.
//
// `appSlug` is the App's slug used in the restriction/bypass actor list.
func (c *AppClient) ConfigureBranchProtection(ctx context.Context, fullName, branch, appSlug, instToken string) error {
	payload := map[string]any{
		"required_status_checks": map[string]any{
			"strict":   true,
			"contexts": []string{},
		},
		"enforce_admins": false,
		"required_pull_request_reviews": map[string]any{
			"required_approving_review_count": 1,
			"bypass_pull_request_allowances": map[string]any{
				"apps": []string{appSlug},
			},
		},
		"restrictions": map[string]any{
			"users": []string{},
			"teams": []string{},
			"apps":  []string{appSlug},
		},
	}
	pb, mErr := json.Marshal(payload)
	if mErr != nil {
		return fwra.Wrap(fwra.ContractMisuse, mErr, "ConfigureBranchProtection: marshal")
	}
	url := fmt.Sprintf("%s/repos/%s/branches/%s/protection", c.baseURL, fullName, branch)
	status, _, dErr := c.do(ctx, http.MethodPut, url, pb, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status < 200 || status >= 300 {
		return ClassifyStatus(status, "ConfigureBranchProtection")
	}
	return nil
}
