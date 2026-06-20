package testinfra

// fakeactions.go is a STATEFUL in-process fake of the GitHub Actions REST surface
// the construction-pipeline RA (C-CP-R) drives: workflow_dispatch, list workflow
// runs (by the dispatched workflow file), get a run, cancel a run. Unlike the
// static-route FakeGitHub (which serves canned responses), the Actions path needs
// STATE — a dispatch must CREATE a run that a subsequent list/get returns — so the
// idempotency-convergence gate can be exercised end-to-end against a faithful
// boundary (a real run-creation-on-dispatch, run-name stamping, and a cancellable
// run), with NO live GitHub.
//
// It models the load-bearing GitHub semantics the RA's idempotency analog depends
// on: (i) a workflow_dispatch with an `idempotency_token` input creates a run whose
// `name` is "aiarch-cp-"+token (the run-name stamping the aiarch workflow file
// performs), and (ii) dispatch is NON-dedup — TWO dispatches with the SAME token
// create TWO runs (exactly GitHub's behaviour, which is why the RA must converge
// them). The fake is concurrency-safe so two goroutines can race a dispatch.
//
// TEST-ONLY: nothing here is imported by production code.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// FakeActions is a stateful fake of the subset of the GitHub Actions REST API the
// construction-pipeline RA uses. Construct with StartActions; address it via
// BaseURL as the AppClient's apiBaseURL.
type FakeActions struct {
	server *httptest.Server

	mu       sync.Mutex
	nextID   int64
	runs     []fakeRun
	requests []RecordedRequest

	// forceStatus, when >0, makes the NEXT matching call return that status with a
	// scripted error body (drives the error-kind mapping cases). It is consumed
	// (reset to 0) on use. Scope it by op via forceOp.
	forceStatus int
	forceOp     string // "dispatch" | "list" | "get" | "cancel" | "" (any)
}

type fakeRun struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
}

// StartActions spins up the stateful Actions fake.
func StartActions() *FakeActions {
	f := &FakeActions{nextID: 1}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// BaseURL is the fake's REST root.
func (f *FakeActions) BaseURL() string { return f.server.URL }

// Close stops the fake.
func (f *FakeActions) Close() { f.server.Close() }

// Requests returns a copy of every request received (for wire-level assertions).
func (f *FakeActions) Requests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// DispatchCount returns how many workflow_dispatch POSTs the fake received — the
// assertion that the RA dispatched at most once on the happy path (and that the
// dedup probe short-circuited a replay without dispatching).
func (f *FakeActions) DispatchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/dispatches") {
			n++
		}
	}
	return n
}

// RunCount returns the number of runs the fake currently holds (one per dispatch).
func (f *FakeActions) RunCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

// SetRunTerminal scripts the run with the given id into a terminal state with the
// given conclusion (drives observe TERMINAL-SUCCESS / FAILURE cases).
func (f *FakeActions) SetRunTerminal(id int64, conclusion string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.runs {
		if f.runs[i].ID == id {
			f.runs[i].Status = "completed"
			f.runs[i].Conclusion = conclusion
		}
	}
}

// SetRunStatus scripts the run's lifecycle status (e.g. "in_progress").
func (f *FakeActions) SetRunStatus(id int64, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.runs {
		if f.runs[i].ID == id {
			f.runs[i].Status = status
		}
	}
}

// ForceNext makes the next call matching op (or any op when op=="") return status
// with a scripted error body — drives the Auth/Transient/etc. error-kind mapping.
func (f *FakeActions) ForceNext(op string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forceOp = op
	f.forceStatus = status
}

var (
	reDispatch = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/workflows/([^/]+)/dispatches$`)
	reListRuns = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/workflows/([^/]+)/runs$`)
	reGetRun   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/runs/(\d+)$`)
	reCancel   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/runs/(\d+)/cancel$`)
)

func (f *FakeActions) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, RecordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Auth: r.Header.Get("Authorization"), Body: string(body),
	})
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && reDispatch.MatchString(r.URL.Path):
		f.handleDispatch(w, body)
	case r.Method == http.MethodGet && reListRuns.MatchString(r.URL.Path):
		f.handleList(w)
	case r.Method == http.MethodGet && reGetRun.MatchString(r.URL.Path):
		f.handleGet(w, reGetRun.FindStringSubmatch(r.URL.Path)[3])
	case r.Method == http.MethodPost && reCancel.MatchString(r.URL.Path):
		f.handleCancel(w, reCancel.FindStringSubmatch(r.URL.Path)[3])
	default:
		writeJSON(w, http.StatusNotFound, `{"message":"fake-actions: no route"}`)
	}
}

// takeForce consumes a scripted forced status if it applies to op; returns (status,
// true) if a force fired.
func (f *FakeActions) takeForce(op string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceStatus == 0 {
		return 0, false
	}
	if f.forceOp != "" && f.forceOp != op {
		return 0, false
	}
	s := f.forceStatus
	f.forceStatus = 0
	f.forceOp = ""
	return s, true
}

func (f *FakeActions) handleDispatch(w http.ResponseWriter, body []byte) {
	if s, forced := f.takeForce("dispatch"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	var payload struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}
	_ = json.Unmarshal(body, &payload)
	token := payload.Inputs["idempotency_token"]

	f.mu.Lock()
	id := f.nextID
	f.nextID++
	// The aiarch workflow file stamps run-name = "aiarch-cp-"+token. A dispatch with
	// NO token (defensive) gets an empty-suffixed name; the RA always supplies one.
	f.runs = append(f.runs, fakeRun{
		ID: id, Name: "aiarch-cp-" + token, Status: "queued",
	})
	f.mu.Unlock()

	// GitHub returns 204 (no body) for workflow_dispatch.
	w.WriteHeader(http.StatusNoContent)
}

func (f *FakeActions) handleList(w http.ResponseWriter) {
	if s, forced := f.takeForce("list"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	f.mu.Lock()
	runs := make([]map[string]any, 0, len(f.runs))
	for _, run := range f.runs {
		runs = append(runs, map[string]any{
			"id": run.ID, "name": run.Name,
			"status": run.Status, "conclusion": run.Conclusion,
		})
	}
	f.mu.Unlock()
	out, _ := json.Marshal(map[string]any{"total_count": len(runs), "workflow_runs": runs})
	writeJSON(w, http.StatusOK, string(out))
}

func (f *FakeActions) handleGet(w http.ResponseWriter, idStr string) {
	if s, forced := f.takeForce("get"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, run := range f.runs {
		if run.ID == id {
			out, _ := json.Marshal(map[string]any{
				"id": run.ID, "name": run.Name,
				"status": run.Status, "conclusion": run.Conclusion,
			})
			writeJSON(w, http.StatusOK, string(out))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, `{"message":"no run"}`)
}

func (f *FakeActions) handleCancel(w http.ResponseWriter, idStr string) {
	if s, forced := f.takeForce("cancel"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.runs {
		if f.runs[i].ID == id {
			if f.runs[i].Status == "completed" {
				// Already terminal — GitHub answers 409 Conflict (the RA maps to success).
				writeJSON(w, http.StatusConflict, `{"message":"cannot cancel a completed run"}`)
				return
			}
			f.runs[i].Status = "completed"
			f.runs[i].Conclusion = "cancelled"
			writeJSON(w, http.StatusAccepted, `{}`)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, `{"message":"no run"}`)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}
