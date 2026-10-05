package scenariohost

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// The live half of the host's Temporal (deterministic-component-testing
// design §12 B2): a manager — and every client that fronts one — is a
// Temporal-backed component, so its REAL collaborator wiring needs the dev
// server's client and a worker polling the manager's task queue. Both come
// from the host, so a hooks file builds the subject through the component's
// public constructor and its composition root's own worker registration and
// never imports the SDK, never dials, never polls, and never reaches into
// the workflow store to clean up after itself.

// TemporalClient is the dev server's client (namespace TemporalNamespace): the
// client a Temporal-backed component's generated constructor takes. The host
// owns it — a hooks file must not Close it.
func (h *Host) TemporalClient() client.Client { return h.temporal }

// StartWorker starts a real worker on the dev server polling taskQueue,
// registers on it through register — the composition root's own wiring, e.g.
// a manager package's RegisterManagerWorker over the subject — and stops it
// when t ends. A registration that panics or a worker that does not start
// fails t.
func (h *Host) StartWorker(t *testing.T, taskQueue string, register func(w Worker)) {
	t.Helper()
	if h.temporal == nil {
		t.Fatal("scenariohost: StartWorker: the host has no Temporal dev server")
	}
	if taskQueue == "" || register == nil {
		t.Fatalf("scenariohost: StartWorker needs a task queue and a registration (got %q, nil=%v)", taskQueue, register == nil)
	}
	w := worker.New(h.temporal, taskQueue, worker.Options{})
	register(w)
	if err := w.Start(); err != nil {
		t.Fatalf("scenariohost: start worker on %q: %v", taskQueue, err)
	}
	t.Cleanup(w.Stop)
}

// terminateTries and terminateTimeout bound endScenarioWorkflows: the
// visibility store trails the history store, so a listing is retried until it
// names no running execution.
const (
	terminateTries   = 10
	terminateTimeout = 30 * time.Second
)

// endScenarioWorkflows terminates every execution still running that was
// started at or after since — the scenario's window. Scenarios of one package
// bind the same ids, and a workflow a scenario left running would answer the
// next scenario's start (or block it), so nothing a scenario started outlives
// it. Terminated histories still replay (B4): termination records no command.
func (h *Host) endScenarioWorkflows(since time.Time) error {
	if h == nil || h.temporal == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminateTimeout)
	defer cancel()
	query := fmt.Sprintf("ExecutionStatus = 'Running' AND StartTime >= %q", since.UTC().Format(time.RFC3339Nano))
	for range terminateTries {
		resp, err := h.temporal.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{Query: query})
		if err != nil {
			return fmt.Errorf("scenariohost: list running executions: %w", err)
		}
		if len(resp.GetExecutions()) == 0 {
			return nil
		}
		for _, e := range resp.GetExecutions() {
			ex := e.GetExecution()
			// A terminate racing the execution's own completion is not a failure.
			_ = h.temporal.TerminateWorkflow(ctx, ex.GetWorkflowId(), ex.GetRunId(), "scenario ended")
		}
		time.Sleep(settleGap)
	}
	return fmt.Errorf("scenariohost: executions started by the scenario are still running after %d terminate passes", terminateTries)
}
