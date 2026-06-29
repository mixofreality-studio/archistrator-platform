package github

// actions.go carries the GITHUB ACTIONS wire plumbing the construction-pipeline
// ResourceAccess (constructionPipelineAccess, C-CP-R) needs to RUN a containerised
// construction pipeline on the user's GitHub Actions instance and observe/cancel
// it. It is the satellite home for the three Actions REST operations that activity
// requires — trigger a workflow_dispatch, list/get workflow runs (to map to a
// pipeline observation), cancel a run — kept HERE (the github satellite), out of
// the product RA, exactly as the App-JWT / installation-token / PR-rail / git-data
// wire code is, so the RA stays provider-opaque.
//
// WHY THE SATELLITE OWNS THIS (CustomerAppInfrastructure governance + C-SC split):
// CI dispatch was explicitly deferred from C-SC (sourceControlAccess) to C-CP-R;
// the GitHub Actions vocabulary (workflow_dispatch, workflow runs, run ids,
// statuses, conclusions, owner/repo) is GitHub-wire lexicon and must live in the
// one sanctioned place GitHub lexemes live — this satellite — never on the RA's
// infrastructure-opaque contract surface (constructionPipelineAccess.md §3, §6).
//
// IDEMPOTENCY ANCHOR — THE RUN-NAME DEDUP TOKEN (the GitHub-Actions analog of
// Argo's "reject duplicate Workflow name"): workflow_dispatch has NO duplicate
// dedup of its own — dispatching twice creates two runs. The convergence the
// frozen contract guarantees (§2.1, §6) is reconstructed by the RA above on a
// DETERMINISTIC anchor this file exposes: the dispatched aiarch construction
// workflow sets `run-name: aiarch-cp-${{ inputs.idempotency_token }}` (the
// documented GitHub dispatch-idempotency recipe), so every run launched for a
// given key carries the SAME queryable run name. ListRunsByName returns every run
// carrying that name; the RA selects the deterministic canonical run (lowest run
// id — a total order both racers compute identically) and cancels the non-canonical
// siblings. That makes two concurrent submits with the same key CONVERGE on one
// handle and one effective run without any atomic dedup primitive. The token<->name
// stamping convention is documented on DispatchInputKeyIdempotency below.
//
// PROVIDER-OPACITY: the value types this file exposes (WorkflowRun, RunStatus,
// RunConclusion, the dispatch/list/cancel methods) carry GitHub-Actions vocabulary
// BY DESIGN — they are the satellite's provider-specific surface. The consuming RA
// (constructionPipelineAccess) maps them to its infrastructure-neutral
// PipelineObservation/PipelineHandle and never lets an Actions lexeme cross its
// own contract. This file speaks GitHub Actions; the RA above it does not.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// DispatchInputKeyIdempotency is the workflow_dispatch input key the RA fills
// with the deterministic dedup token derived from the caller-supplied
// idempotencyKey. The aiarch construction workflow file is authored to declare this
// input and to stamp it into its run name via
//
//	run-name: aiarch-cp-${{ inputs.idempotency_token }}
//
// so the launched run's queryable `name` carries the token (RunNamePrefix+token).
// This is the load-bearing idempotency anchor (see file header).
const DispatchInputKeyIdempotency = "idempotency_token"

// RunNamePrefix prefixes the deterministic run name the dispatched workflow stamps
// from the idempotency token. ListRunsByName matches on RunNamePrefix+token.
const RunNamePrefix = "aiarch-cp-"

// RunStatus is the lifecycle status of a GitHub Actions workflow run (the run
// object's `status` field). GitHub's vocabulary is open-ended; the values below are
// the ones the construction-pipeline observe path maps. Unknown/empty maps to "".
type RunStatus string

const (
	// RunQueued — the run is queued (queued / requested / waiting / pending all
	// collapse here from the RA's POV: "submitted, not yet executing").
	RunQueued RunStatus = "queued"
	// RunInProgress — one or more jobs are executing.
	RunInProgress RunStatus = "in_progress"
	// RunCompleted — the run reached a terminal state; read Conclusion for outcome.
	RunCompleted RunStatus = "completed"
)

// RunConclusion is the terminal outcome of a completed run (the run object's
// `conclusion` field, meaningful only once Status == completed).
type RunConclusion string

const (
	RunSuccess        RunConclusion = "success"
	RunFailure        RunConclusion = "failure"
	RunCancelled      RunConclusion = "cancelled"
	RunSkipped        RunConclusion = "skipped"
	RunTimedOut       RunConclusion = "timed_out"
	RunActionRequired RunConclusion = "action_required"
	RunNeutral        RunConclusion = "neutral"
	RunStartupFailure RunConclusion = "startup_failure"
)

// WorkflowRun is the satellite's view of a GitHub Actions workflow run — only the
// fields the construction-pipeline observe/dedup path reads. It is GitHub-specific
// (the RA maps it to PipelineObservation). NOTE the RA NEVER sees raw logs; the
// run's terminal outcome + a short title is the decision input.
type WorkflowRun struct {
	// ID is the GitHub run id — the durable per-run address. The RA packs it into
	// its opaque PipelineHandle (with owner/repo) and never parses it as a number.
	ID int64
	// Name is the run's display name. For an aiarch construction dispatch it equals
	// RunNamePrefix+token (the idempotency anchor); ListRunsByName filters on it.
	Name string
	// Status is the run lifecycle status.
	Status RunStatus
	// Conclusion is the terminal outcome (empty until Status == completed).
	Conclusion RunConclusion
}

// ---------------------------------------------------------------------------
// Wire DTOs — package-internal JSON views of the Actions REST shapes. None
// crosses the RA contract surface.
// ---------------------------------------------------------------------------

type workflowRunDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type listRunsDTO struct {
	TotalCount   int              `json:"total_count"`
	WorkflowRuns []workflowRunDTO `json:"workflow_runs"`
}

func (d workflowRunDTO) toWorkflowRun() WorkflowRun {
	return WorkflowRun{
		ID:         d.ID,
		Name:       d.Name,
		Status:     RunStatus(d.Status),
		Conclusion: RunConclusion(d.Conclusion),
	}
}

// ---------------------------------------------------------------------------
// GitHub Actions REST calls (the C-CP-R back contract).
//
// Every call authenticates with a CALLER-SUPPLIED installation token (instToken)
// — the same credential discipline the git-data path uses. The satellite mints
// nothing here; the RA's owning component supplies the installation token (it is
// constructed with the App identity and mints the token internally, never via a
// sideways RA call).
// ---------------------------------------------------------------------------

// DispatchWorkflow triggers a workflow_dispatch event for the named workflow file
// on `ref`, passing `inputs` (which MUST include DispatchInputKeyIdempotency
// so the launched run carries the deterministic dedup name). GitHub creates the run
// ASYNCHRONOUSLY and the dispatch response carries no reliable run id, so the caller
// resolves the run afterward via ListRunsByName. A non-2xx maps via ClassifyStatus.
//
//	POST /repos/{owner}/{repo}/actions/workflows/{workflowFile}/dispatches
func (c *AppClient) DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string, inputs map[string]string, instToken string) error {
	if strings.TrimSpace(workflowFile) == "" {
		return fwra.New(fwra.ContractMisuse, "DispatchWorkflow: empty workflow file")
	}
	if strings.TrimSpace(ref) == "" {
		return fwra.New(fwra.ContractMisuse, "DispatchWorkflow: empty ref")
	}
	payload := map[string]any{"ref": ref}
	if len(inputs) > 0 {
		payload["inputs"] = inputs
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fwra.Wrap(fwra.ContractMisuse, err, "DispatchWorkflow: marshal")
	}
	u := fmt.Sprintf("%s/repos/%s/%s/actions/workflows/%s/dispatches",
		c.baseURL, owner, repo, url.PathEscape(workflowFile))
	status, _, dErr := c.do(ctx, http.MethodPost, u, body, "", instToken)
	if dErr != nil {
		return dErr
	}
	if status < 200 || status >= 300 {
		return ClassifyStatus(status, "DispatchWorkflow")
	}
	return nil
}

// ListRunsByName lists workflow runs of the named workflow file and returns every
// run whose display name == runName (the idempotency anchor RunNamePrefix+token).
// It is the dedup probe (before dispatch) and the post-dispatch run resolver. An
// empty result is not an error (no run yet exists for the key). A non-2xx maps via
// ClassifyStatus.
//
//	GET /repos/{owner}/{repo}/actions/workflows/{workflowFile}/runs?event=workflow_dispatch&per_page=100
//
// Scoping the list to the workflow file + the workflow_dispatch event keeps the
// scan bounded; matching on the exact run name is the deterministic selector.
func (c *AppClient) ListRunsByName(ctx context.Context, owner, repo, workflowFile, runName, instToken string) ([]WorkflowRun, error) {
	if strings.TrimSpace(runName) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "ListRunsByName: empty runName")
	}
	u := fmt.Sprintf("%s/repos/%s/%s/actions/workflows/%s/runs?event=workflow_dispatch&per_page=100",
		c.baseURL, owner, repo, url.PathEscape(workflowFile))
	status, respBody, err := c.do(ctx, http.MethodGet, u, nil, "", instToken)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, ClassifyStatus(status, "ListRunsByName")
	}
	var list listRunsDTO
	if uerr := json.Unmarshal(respBody, &list); uerr != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, uerr, "ListRunsByName: decode")
	}
	out := make([]WorkflowRun, 0, len(list.WorkflowRuns))
	for _, d := range list.WorkflowRuns {
		if d.Name == runName {
			out = append(out, d.toWorkflowRun())
		}
	}
	return out, nil
}

// GetRun fetches one workflow run by id. A missing run surfaces as fwra.NotFound
// (via ClassifyStatus's 404 mapping). A non-2xx maps via ClassifyStatus.
//
//	GET /repos/{owner}/{repo}/actions/runs/{runID}
func (c *AppClient) GetRun(ctx context.Context, owner, repo string, runID int64, instToken string) (WorkflowRun, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", c.baseURL, owner, repo, runID)
	status, respBody, err := c.do(ctx, http.MethodGet, u, nil, "", instToken)
	if err != nil {
		return WorkflowRun{}, err
	}
	if status < 200 || status >= 300 {
		return WorkflowRun{}, ClassifyStatus(status, "GetRun")
	}
	var d workflowRunDTO
	if uerr := json.Unmarshal(respBody, &d); uerr != nil {
		return WorkflowRun{}, fwra.Wrap(fwra.Infrastructure, uerr, "GetRun: decode")
	}
	return d.toWorkflowRun(), nil
}

// CancelRun requests cancellation of the run named by runID. GitHub returns 202
// (accepted) on success and 409 (conflict) when the run is already finished /
// not cancellable; the latter is mapped to SUCCESS here because the desired
// post-condition — "this run is no longer progressing" — already holds (the RA's
// cancel verb is idempotent-on-intent). A 404 (run already gone) likewise maps to
// SUCCESS. Other non-2xx map via ClassifyStatus.
//
//	POST /repos/{owner}/{repo}/actions/runs/{runID}/cancel
func (c *AppClient) CancelRun(ctx context.Context, owner, repo string, runID int64, instToken string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/cancel", c.baseURL, owner, repo, runID)
	status, _, err := c.do(ctx, http.MethodPost, u, []byte("{}"), "", instToken)
	if err != nil {
		return err
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusConflict, status == http.StatusNotFound:
		// Already-terminal / already-gone == cancelled (idempotent-on-intent).
		return nil
	default:
		return ClassifyStatus(status, "CancelRun")
	}
}
