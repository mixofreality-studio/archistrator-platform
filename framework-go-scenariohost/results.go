package scenariohost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// resultsFile is the index every scenario run leaves behind, one per component
// under ResultsDir. Its shape is the TestRun contract: the renderer on the
// testing task reads nothing else.
const resultsFile = "results.json"

// Status values a ScenarioResult may carry.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// ScenarioResult is one scenario's verdict as written to results.json. The
// JSON field names are the wire contract shared with the methodcheck mirror
// type of the same name and with Playwright's reporter, which writes the same
// shape (with VideoRef / TraceRef) for UI clients.
type ScenarioResult struct {
	Scenario   string `json:"scenario"`
	Status     string `json:"status"` // pass|fail|skip
	DurationMs int    `json:"durationMs"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	VideoRef   string `json:"videoRef,omitempty"`
	TraceRef   string `json:"traceRef,omitempty"`
}

// Results collects per-scenario verdicts and writes results.json on flush. It
// is embedded in Host so a Host records and flushes directly; a zero value is
// ready to use.
type Results struct {
	mu      sync.Mutex
	entries map[string]ScenarioResult
}

// Record stores r under its scenario id, replacing an earlier verdict for the
// same scenario (a re-run supersedes, it does not duplicate).
func (h *Host) Record(r ScenarioResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = map[string]ScenarioResult{}
	}
	h.entries[r.Scenario] = r
}

// Flush writes ResultsDir/results.json: every recorded verdict, sorted by
// scenario id, as an indented JSON array ("[]" when nothing was recorded, so a
// package with zero bound scenarios still leaves an index behind).
func (h *Host) Flush() error {
	h.mu.Lock()
	out := make([]ScenarioResult, 0, len(h.entries))
	for _, r := range h.entries {
		out = append(out, r)
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Scenario < out[j].Scenario })

	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("scenariohost: encode results: %w", err)
	}
	if err := os.MkdirAll(h.ResultsDir, 0o750); err != nil {
		return fmt.Errorf("scenariohost: mkdir %s: %w", h.ResultsDir, err)
	}
	path := filepath.Join(h.ResultsDir, resultsFile)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("scenariohost: write %s: %w", path, err)
	}
	return nil
}

// RunScenario runs fn as the sub-test named id and records its verdict:
// pass, fail or skip (a skip after a failure stays a fail, as in `go test`),
// plus the wall-clock duration. A panic inside fn is recorded as a fail and
// re-raised so the testing package reports it as usual.
//
// The sub-test's own log (t.Log, the failure message) is NOT captured here:
// the testing package offers no in-process hook on a test's log stream, so
// Stdout / Stderr stay empty and the venue's raw `go test -json` stream, which
// is uploaded next to results.json, carries that text.
func (h *Host) RunScenario(t *testing.T, id string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(id, func(t *testing.T) {
		start := time.Now()
		defer func() {
			r := ScenarioResult{Scenario: id, DurationMs: int(time.Since(start) / time.Millisecond)}
			panicked := recover()
			switch {
			case panicked != nil, t.Failed():
				r.Status = StatusFail
			case t.Skipped():
				r.Status = StatusSkip
			default:
				r.Status = StatusPass
			}
			h.Record(r)
			if panicked != nil {
				panic(panicked)
			}
		}()
		fn(t)
	})
}
