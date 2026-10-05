package scenariohost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// resultsFile is the index every scenario run leaves behind, one per component
// under ResultsDir(component). Its shape is the TestRun contract: the renderer
// on the testing task reads nothing else.
const resultsFile = "results.json"

// replayTimeout bounds one flush's replay pass (listing, fetching and
// replaying every new history).
const replayTimeout = 2 * time.Minute

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

// resultKey is what a verdict is stored under. Several components (facets)
// can share one Go package — one test process — and bind the SAME scenario id,
// so the scenario id alone would let one facet's verdict overwrite another's.
type resultKey struct{ component, scenario string }

// Results collects per-(component, scenario) verdicts and writes one
// results.json per component on flush. It is embedded in Host so a Host
// records and flushes directly; a zero value is ready to use.
type Results struct {
	mu       sync.Mutex
	entries  map[resultKey]ScenarioResult
	declared map[string]bool // components that get an index even with no verdict
	windows  map[resultKey]window
	// replayFailed holds the replay gate's failures per scenario (replay.go):
	// they override the recorded verdict at every flush, so a re-run that
	// passes cannot wash a non-deterministic history out.
	replayFailed map[resultKey][]string
}

// Record stores r under (component, r.Scenario), replacing an earlier verdict
// for the same pair (a re-run supersedes, it does not duplicate).
func (h *Host) Record(component string, r ScenarioResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = map[resultKey]ScenarioResult{}
	}
	h.entries[resultKey{component, r.Scenario}] = r
}

// ran records the wall-clock window (component, scenario) last ran in; the
// replay gate attributes a workflow execution to the scenario whose window
// holds its start.
func (h *Host) ran(component, scenario string, w window) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.windows == nil {
		h.windows = map[resultKey]window{}
	}
	h.windows[resultKey{component, scenario}] = w
}

// scenarioAt is the scenario whose run window holds t.
func (h *Host) scenarioAt(t time.Time) (resultKey, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, w := range h.windows {
		if !t.Before(w.start) && !t.After(w.end) {
			return k, true
		}
	}
	return resultKey{}, false
}

// failReplay marks (component, scenario) failed by the replay gate with msg.
func (h *Host) failReplay(k resultKey, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replayFailed == nil {
		h.replayFailed = map[resultKey][]string{}
	}
	h.replayFailed[k] = append(h.replayFailed[k], msg)
}

// declare names components whose index Flush writes even when none of their
// scenarios recorded a verdict, so a component with zero bound scenarios still
// leaves an index behind.
func (h *Host) declare(components ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.declared == nil {
		h.declared = map[string]bool{}
	}
	for _, c := range components {
		h.declared[c] = true
	}
}

// ResultsDir is component's results directory, ResultsRoot/<component>/: the
// one tree its results.json (and, for a UI client, its videos and traces)
// lands in.
func (h *Host) ResultsDir(component string) string {
	return filepath.Join(h.ResultsRoot, component)
}

// Flush first runs the replay gate (replay.go) when the host has one, then
// writes ResultsDir(c)/results.json for every declared or recorded component
// c: that component's verdicts, sorted by scenario id, as an indented JSON
// array ("[]" for a declared component that recorded nothing). A scenario the
// replay gate failed is written as a fail whose Stderr names each history
// that did not replay. A verdict recorded without a component fails the flush
// before anything is written: it belongs to no index. The error joins the
// replay failures this flush found (each is reported by one flush only) with
// any write failure.
func (h *Host) Flush() error {
	ctx, cancel := context.WithTimeout(context.Background(), replayTimeout)
	defer cancel()
	replayErr := h.replayHistories(ctx)
	return errors.Join(replayErr, h.writeIndexes())
}

// writeIndexes writes every component's results.json (Flush).
func (h *Host) writeIndexes() error {
	byComponent, orphan := h.verdictsByComponent()
	if len(orphan) > 0 {
		sort.Strings(orphan)
		return fmt.Errorf("scenariohost: verdicts recorded with no component: %s", strings.Join(orphan, ", "))
	}

	components := make([]string, 0, len(byComponent))
	for c := range byComponent {
		components = append(components, c)
	}
	sort.Strings(components)
	for _, c := range components {
		if err := h.writeIndex(c, byComponent[c]); err != nil {
			return err
		}
	}
	return nil
}

// verdictsByComponent groups the verdicts by component — every declared
// component present, a replay-failed scenario turned into a fail carrying
// the replay findings — and names the scenarios recorded with no component.
func (h *Host) verdictsByComponent() (byComponent map[string][]ScenarioResult, orphan []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	byComponent = map[string][]ScenarioResult{}
	for c := range h.declared {
		byComponent[c] = []ScenarioResult{}
	}
	for k, r := range h.entries {
		if k.component == "" {
			orphan = append(orphan, k.scenario)
			continue
		}
		if msgs := h.replayFailed[k]; len(msgs) > 0 {
			r.Status = StatusFail
			r.Stderr = strings.Join(append(nonEmpty(r.Stderr), msgs...), "\n")
		}
		byComponent[k.component] = append(byComponent[k.component], r)
	}
	return byComponent, orphan
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// writeIndex writes one component's verdicts, sorted by scenario id.
func (h *Host) writeIndex(component string, out []ScenarioResult) error {
	sort.Slice(out, func(i, j int) bool { return out[i].Scenario < out[j].Scenario })
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("scenariohost: encode %s results: %w", component, err)
	}
	dir := h.ResultsDir(component)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("scenariohost: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, resultsFile)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("scenariohost: write %s: %w", path, err)
	}
	return nil
}

// RunScenario runs fn as the sub-test named id and records its verdict under
// (component, id) — component is the contract the scenario was projected for,
// one of the package's facets when several share it (a verdict with no
// component fails the next Flush):
// pass, fail or skip (a skip after a failure stays a fail, as in `go test`),
// plus the wall-clock duration. A panic inside fn is recorded as a fail and
// re-raised so the testing package reports it as usual. The run's window is
// kept too: the replay gate attributes every workflow execution started in it
// to this scenario.
//
// The sub-test's own log (t.Log, the failure message) is NOT captured here:
// the testing package offers no in-process hook on a test's log stream, so
// Stdout / Stderr stay empty (but for the replay gate's findings) and the
// venue's raw `go test -json` stream, which is uploaded next to results.json,
// carries that text.
func (h *Host) RunScenario(t *testing.T, component, id string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(id, func(t *testing.T) {
		start := time.Now()
		defer func() {
			r := ScenarioResult{Scenario: id, DurationMs: int(time.Since(start) / time.Millisecond)}
			panicked := recover()
			// Nothing the scenario started outlives it (temporal.go); ending it
			// before the verdict lets a failure to end it fail the scenario.
			if err := h.endScenarioWorkflows(start); err != nil {
				t.Error(err)
			}
			switch {
			case panicked != nil, t.Failed():
				r.Status = StatusFail
			case t.Skipped():
				r.Status = StatusSkip
			default:
				r.Status = StatusPass
			}
			h.Record(component, r)
			h.ran(component, id, window{start: start, end: time.Now()})
			if panicked != nil {
				panic(panicked)
			}
		}()
		fn(t)
	})
}
