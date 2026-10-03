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
// It also models the run-ARTIFACT surface (list a run's artifacts, download one as
// a zip) with GitHub's load-bearing wire fact: the zip route does NOT serve bytes,
// it answers 302 to blob storage on a DIFFERENT host. The fake therefore runs a
// second httptest server (the "blob host") that records what reached it, so the
// client's never-forward-the-installation-token redirect rule is testable.
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
	// blob is the SECOND host — the stand-in for GitHub's artifact blob storage.
	// The artifact zip route answers 302 to it, exactly as GitHub does, so the
	// client's token-safe redirect handling is exercised against a different host.
	blob *httptest.Server

	mu           sync.Mutex
	nextID       int64
	runs         []fakeRun
	artifacts    []fakeArtifact
	requests     []RecordedRequest
	blobRequests []RecordedRequest

	// forceStatus, when >0, makes the NEXT matching call return that status with a
	// scripted error body (drives the error-kind mapping cases). It is consumed
	// (reset to 0) on use. Scope it by op via forceOp.
	forceStatus int
	forceOp     string // "dispatch" | "list" | "get" | "cancel" | "listArtifacts" | "downloadArtifact" | "" (any)
}

type fakeRun struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
}

type fakeArtifact struct {
	ID   int64
	Name string
	Zip  []byte
}

// StartActions spins up the stateful Actions fake (API host + blob host).
func StartActions() *FakeActions {
	f := &FakeActions{nextID: 1}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	f.blob = httptest.NewServer(http.HandlerFunc(f.handleBlob))
	return f
}

// BaseURL is the fake's REST root.
func (f *FakeActions) BaseURL() string { return f.server.URL }

// Close stops the fake (both hosts).
func (f *FakeActions) Close() {
	f.server.Close()
	f.blob.Close()
}

// Requests returns a copy of every request the API host received (for wire-level
// assertions).
func (f *FakeActions) Requests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// BlobRequests returns a copy of every request the BLOB host received — the
// assertion surface for "the installation token never crossed to the blob host".
func (f *FakeActions) BlobRequests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.blobRequests))
	copy(out, f.blobRequests)
	return out
}

// AddArtifact registers an artifact (listed under EVERY run id — the fake does not
// model run↔artifact ownership) whose zip bytes the blob host serves.
func (f *FakeActions) AddArtifact(id int64, name string, zip []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artifacts = append(f.artifacts, fakeArtifact{ID: id, Name: name, Zip: zip})
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

	reListArtifacts = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/runs/(\d+)/artifacts$`)
	reArtifactZip   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/actions/artifacts/(\d+)/zip$`)
	reBlob          = regexp.MustCompile(`^/blob/(\d+)$`)
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
	case r.Method == http.MethodGet && reListArtifacts.MatchString(r.URL.Path):
		f.handleListArtifacts(w)
	case r.Method == http.MethodGet && reArtifactZip.MatchString(r.URL.Path):
		f.handleArtifactZip(w, r, reArtifactZip.FindStringSubmatch(r.URL.Path)[3])
	default:
		writeJSON(w, http.StatusNotFound, `{"message":"fake-actions: no route"}`)
	}
}

// handleBlob is the blob host: it records every request (so a test can assert no
// credential crossed hosts) and serves the registered zip bytes.
func (f *FakeActions) handleBlob(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.blobRequests = append(f.blobRequests, RecordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Auth: r.Header.Get("Authorization"), Body: string(body),
	})
	f.mu.Unlock()

	m := reBlob.FindStringSubmatch(r.URL.Path)
	if r.Method != http.MethodGet || m == nil {
		writeJSON(w, http.StatusNotFound, `{"message":"fake-blob: no route"}`)
		return
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.artifacts {
		if a.ID == id {
			w.Header().Set("Content-Type", "application/zip")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(a.Zip)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, `{"message":"no blob"}`)
}

func (f *FakeActions) handleListArtifacts(w http.ResponseWriter) {
	if s, forced := f.takeForce("listArtifacts"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	f.mu.Lock()
	arts := make([]map[string]any, 0, len(f.artifacts))
	for _, a := range f.artifacts {
		arts = append(arts, map[string]any{
			"id": a.ID, "name": a.Name, "size_in_bytes": len(a.Zip),
			"expired": false, "created_at": "2026-10-02T00:00:00Z",
		})
	}
	f.mu.Unlock()
	out, _ := json.Marshal(map[string]any{"total_count": len(arts), "artifacts": arts})
	writeJSON(w, http.StatusOK, string(out))
}

// handleArtifactZip answers exactly as GitHub does: 302 to blob storage (a
// DIFFERENT host) — never the bytes themselves.
func (f *FakeActions) handleArtifactZip(w http.ResponseWriter, r *http.Request, idStr string) {
	if s, forced := f.takeForce("downloadArtifact"); forced {
		writeJSON(w, s, `{"message":"forced"}`)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	f.mu.Lock()
	found := false
	for _, a := range f.artifacts {
		if a.ID == id {
			found = true
			break
		}
	}
	f.mu.Unlock()
	if !found {
		writeJSON(w, http.StatusNotFound, `{"message":"no artifact"}`)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/blob/%d", f.blob.URL, id), http.StatusFound)
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
