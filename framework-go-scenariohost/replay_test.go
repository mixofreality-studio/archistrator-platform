package scenariohost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// The probe workflow pair: v1 is what the live worker runs, v2 is the same
// workflow type after a non-deterministic edit (an activity became a timer).
// Registering v2 with the replay gate while v1 ran is exactly a workflow
// code change that no GetVersion guards.
const probeActivityName = "scenariohostProbeActivity"

func probeActivity(_ context.Context) (string, error) { return "ok", nil }

func probeWorkflowV1(ctx workflow.Context) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: 10 * time.Second})
	var out string
	err := workflow.ExecuteActivity(ctx, probeActivityName).Get(ctx, &out)
	return out, err
}

func probeWorkflowV2(ctx workflow.Context) (string, error) {
	if err := workflow.Sleep(ctx, time.Millisecond); err != nil {
		return "", err
	}
	return "ok", nil
}

// probeBlockingWorkflow runs one activity, then waits for a signal that never
// comes: at Flush its history is partial (the execution is still running).
func probeBlockingWorkflow(ctx workflow.Context) error {
	if _, err := probeWorkflowV1(ctx); err != nil {
		return err
	}
	workflow.GetSignalChannel(ctx, "never").Receive(ctx, nil)
	return nil
}

// liveWorker starts a worker on the shared dev server running fn under
// wfType, on a task queue of its own, and stops it when t ends.
func liveWorker(t *testing.T, shared *Host, wfType string, fn any) string {
	t.Helper()
	tq := "scenariohost-" + wfType
	w := worker.New(shared.temporal, tq, worker.Options{})
	w.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: wfType})
	w.RegisterActivityWithOptions(probeActivity, activity.RegisterOptions{Name: probeActivityName})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	return tq
}

// gatedHost is a Host of its own (its own results tree and scenario windows)
// on the shared dev server, gated by a replayer that registers fn under
// wfType.
func gatedHost(t *testing.T, shared *Host, wfType string, fn any) *Host {
	t.Helper()
	gate, err := newReplayGate(Workflows{Register: func(w Worker) {
		w.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: wfType})
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &Host{
		ResultsRoot:       t.TempDir(),
		TemporalHostPort:  shared.TemporalHostPort,
		TemporalNamespace: shared.TemporalNamespace,
		temporal:          shared.temporal,
		booted:            time.Now(),
		replay:            gate,
	}
}

// runProbe starts wfType on tq and waits for its result.
func runProbe(t *testing.T, c client.Client, tq, wfType, id string) client.WorkflowRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: tq}, wfType)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func uniqueID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000")
}

// TestReplayGateFailsTheScenarioWhoseWorkflowDrifted is B4: the host replays
// every history a scenario started against the registered workflows, and a
// non-determinism error fails THAT scenario, naming the history — in the
// Flush error (so `go test` fails) and in the component's results.json.
func TestReplayGateFailsTheScenarioWhoseWorkflowDrifted(t *testing.T) {
	shared := Start(t)
	const wfType = "scenariohostProbeDrift"
	tq := liveWorker(t, shared, wfType, probeWorkflowV1)
	h := gatedHost(t, shared, wfType, probeWorkflowV2)

	id := uniqueID("drift")
	var runID string
	h.RunScenario(t, "probe", "drift-P1", func(t *testing.T) {
		run := runProbe(t, shared.temporal, tq, wfType, id)
		var out string
		if err := run.Get(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		runID = run.GetRunID()
	})
	h.RunScenario(t, "probe", "other-P1", func(t *testing.T) {})

	err := h.Flush()
	if err == nil {
		t.Fatal("a drifted workflow history must fail the flush")
	}
	history := id + "/" + runID
	// TMPRL1100 is the SDK's non-determinism error code.
	for _, want := range []string{"probe", "drift-P1", history, "TMPRL1100"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("flush error lacks %q: %v", want, err)
		}
	}
	got := readIndex(t, h.ResultsRoot, "probe")
	if len(got) != 2 || got[0].Scenario != "drift-P1" || got[0].Status != StatusFail || !strings.Contains(got[0].Stderr, history) {
		t.Fatalf("drift-P1 must be a fail naming history %s: %+v", history, got)
	}
	if got[1].Scenario != "other-P1" || got[1].Status != StatusPass {
		t.Fatalf("a scenario that started no drifted workflow keeps its verdict: %+v", got[1])
	}

	// Reported once: a later flush does not fail again, and the verdict sticks.
	if err := h.Flush(); err != nil {
		t.Fatalf("a replay failure is reported by the flush that found it, once: %v", err)
	}
	if got := readIndex(t, h.ResultsRoot, "probe"); got[0].Status != StatusFail {
		t.Fatalf("the replay verdict must survive later flushes: %+v", got)
	}
}

// TestReplayGateFailsAReRunScenarioStill: a re-recorded verdict for the same
// (component, scenario) does not wash out a replay failure.
func TestReplayGateFailsAReRunScenarioStill(t *testing.T) {
	shared := Start(t)
	const wfType = "scenariohostProbeRerun"
	tq := liveWorker(t, shared, wfType, probeWorkflowV1)
	h := gatedHost(t, shared, wfType, probeWorkflowV2)
	id := uniqueID("rerun")
	h.RunScenario(t, "probe", "rerun-P1", func(t *testing.T) {
		if err := runProbe(t, shared.temporal, tq, wfType, id).Get(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	})
	if err := h.Flush(); err == nil {
		t.Fatal("drift not detected")
	}
	h.Record("probe", ScenarioResult{Scenario: "rerun-P1", Status: StatusPass})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, h.ResultsRoot, "probe"); len(got) != 1 || got[0].Status != StatusFail {
		t.Fatalf("%+v", got)
	}
}

// TestReplayGatePassesAWorkflowThatReplays: the same code replays clean, so
// the scenario keeps its pass and the flush succeeds.
func TestReplayGatePassesAWorkflowThatReplays(t *testing.T) {
	shared := Start(t)
	const wfType = "scenariohostProbeClean"
	tq := liveWorker(t, shared, wfType, probeWorkflowV1)
	h := gatedHost(t, shared, wfType, probeWorkflowV1)
	h.RunScenario(t, "probe", "clean-P1", func(t *testing.T) {
		if err := runProbe(t, shared.temporal, tq, wfType, uniqueID("clean")).Get(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, h.ResultsRoot, "probe"); len(got) != 1 || got[0].Status != StatusPass || got[0].Stderr != "" {
		t.Fatalf("%+v", got)
	}
	if n := len(h.replay.done); n != 1 {
		t.Fatalf("the one closed execution must be replayed and settled, got %d", n)
	}
}

// TestReplayGateReplaysARunningWorkflowsPartialHistory: a workflow still
// running at Flush replays up to its last event — clean when the code is
// unchanged, a fail when it drifted — and a clean one is replayed again at
// the next flush (it is not settled).
func TestReplayGateReplaysARunningWorkflowsPartialHistory(t *testing.T) {
	shared := Start(t)
	const wfType = "scenariohostProbeRunning"
	tq := liveWorker(t, shared, wfType, probeBlockingWorkflow)
	h := gatedHost(t, shared, wfType, probeBlockingWorkflow)
	id := uniqueID("running")
	// Terminated when the TEST ends, not the scenario: it must still be
	// running at Flush.
	t.Cleanup(func() { _ = shared.temporal.TerminateWorkflow(context.Background(), id, "", "probe done") })
	h.RunScenario(t, "probe", "running-P1", func(t *testing.T) {
		runProbe(t, shared.temporal, tq, wfType, id)
		waitForActivityCompleted(t, shared.temporal, id)
	})
	if err := h.Flush(); err != nil {
		t.Fatal(err)
	}
	if n := len(h.replay.done); n != 0 {
		t.Fatalf("a running execution must not be settled, got %d settled", n)
	}
	if got := readIndex(t, h.ResultsRoot, "probe"); len(got) != 1 || got[0].Status != StatusPass {
		t.Fatalf("%+v", got)
	}

	// The same partial history against drifted code is a fail: a running
	// execution is compared, not passed over.
	drifted := gatedHost(t, shared, wfType, probeWorkflowV2)
	drifted.booted = h.booted
	drifted.windows = h.windows
	drifted.entries = h.entries
	if err := drifted.Flush(); err == nil || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "TMPRL1100") {
		t.Fatalf("a drifted running workflow must fail the flush, naming it: %v", err)
	}
}

// waitForActivityCompleted polls id's history until its activity completed,
// so the partial history carries a completed workflow task with commands.
func waitForActivityCompleted(t *testing.T, c client.Client, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		it := c.GetWorkflowHistory(t.Context(), id, "", false, 0)
		for it.HasNext() {
			ev, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if ev.GetActivityTaskCompletedEventAttributes() != nil {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workflow %s never completed its activity", id)
}

// TestReplayGateFailsAHistoryOutsideEveryScenario: a drifted history no
// scenario window holds still fails the flush, naming the history.
func TestReplayGateFailsAHistoryOutsideEveryScenario(t *testing.T) {
	shared := Start(t)
	const wfType = "scenariohostProbeStray"
	tq := liveWorker(t, shared, wfType, probeWorkflowV1)
	h := gatedHost(t, shared, wfType, probeWorkflowV2)
	id := uniqueID("stray")
	if err := runProbe(t, shared.temporal, tq, wfType, id).Get(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	err := h.Flush()
	if err == nil || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "no scenario") {
		t.Fatalf("a stray drifted history must fail the flush, naming it: %v", err)
	}
}

// TestReplayGateIgnoresUnregisteredWorkflowTypes: the shared namespace holds
// other packages' and other runs' executions; only the types this package
// registered are its to replay.
func TestReplayGateIgnoresUnregisteredWorkflowTypes(t *testing.T) {
	shared := Start(t)
	const live, gated = "scenariohostProbeForeign", "scenariohostProbeNotRun"
	tq := liveWorker(t, shared, live, probeWorkflowV1)
	h := gatedHost(t, shared, gated, probeWorkflowV2)
	h.RunScenario(t, "probe", "foreign-P1", func(t *testing.T) {
		if err := runProbe(t, shared.temporal, tq, live, uniqueID("foreign")).Get(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	})
	if err := h.Flush(); err != nil {
		t.Fatalf("a foreign workflow type is not this package's to replay: %v", err)
	}
}

func TestReplayRegistryRecordsWorkflowTypesAndNeverPolls(t *testing.T) {
	gate, err := newReplayGate(Workflows{Register: func(w Worker) {
		w.RegisterWorkflow(probeWorkflowV1)
		w.RegisterWorkflowWithOptions(probeWorkflowV2, workflow.RegisterOptions{Name: "named"})
		w.RegisterActivity(probeActivity)
		w.RegisterActivityWithOptions(probeActivity, activity.RegisterOptions{Name: "x"})
		if err := w.Start(); err == nil {
			t.Error("the replay registry must refuse to Start")
		}
		if err := w.Run(nil); err == nil {
			t.Error("the replay registry must refuse to Run")
		}
		w.Stop()
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"probeWorkflowV1", "named"} {
		if !gate.types[want] {
			t.Errorf("type %q not recorded: %v", want, gate.types)
		}
	}
	if len(gate.types) != 2 || gate.dynamic {
		t.Errorf("activities are not workflow types: %v dynamic=%v", gate.types, gate.dynamic)
	}
}

func TestNewReplayGateRefusesAnEmptyOrPanickingRegistration(t *testing.T) {
	for name, wf := range map[string]Workflows{
		"nil":      {},
		"empty":    {Register: func(Worker) {}},
		"panicked": {Register: func(Worker) { panic("FILL replayWorkflows") }},
	} {
		if _, err := newReplayGate(wf); err == nil {
			t.Errorf("%s registration accepted", name)
		}
	}
	_, err := newReplayGate(Workflows{Register: func(Worker) { panic("FILL replayWorkflows") }})
	if err == nil || !strings.Contains(err.Error(), "FILL replayWorkflows") {
		t.Fatalf("the panic must be carried: %v", err)
	}
}

// registerHelperEnv marks the re-executed test binary whose TestMain hands
// MainWithWorkflows a registration that panics (an unfilled hooks stub).
const registerHelperEnv = "SCENARIOHOST_REGISTER_HELPER"

func TestMainWithWorkflowsExitsOnABrokenRegistration(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), registerHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("want exit 2, got %v:\n%s", err, out)
	}
	if !strings.Contains(string(out), "FILL replayWorkflows") {
		t.Fatalf("the exit must name the broken registration:\n%s", out)
	}
}
