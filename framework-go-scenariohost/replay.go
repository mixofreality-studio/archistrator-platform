package scenariohost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	nexus "github.com/nexus-rpc/sdk-go/nexus"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	tlog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// The replay gate (deterministic-component-testing design §12 B4): workflow
// determinism stays gated, black-box. A manager package's TestMain hands the
// host its workflows (MainWithWorkflows); at every Flush the host lists every
// workflow execution of a registered type started on the dev server since it
// booted, fetches each history and replays it with the Temporal SDK's
// WorkflowReplayer against those registrations — the CURRENT worker code, in
// the same test run. A replay error (a non-determinism error above all) marks
// the scenario whose run window holds the execution's start `fail`, naming
// the history, and fails the flush that found it.

// Worker is the registry a Workflows.Register func registers on. It is the
// SDK's worker.Worker, so a manager package's own worker wiring (the function
// its composition root calls on a live worker) registers here unchanged; the
// alias spares the hooks file an SDK import. Workflow registrations land on
// the replayer; activity and Nexus registrations are accepted and ignored (a
// replay never executes them); Start and Run fail — it never polls.
type Worker = worker.Worker

// ReplayerOptions configures the replayer the way the package's live workers
// are configured: the DataConverter, FailureConverter, ContextPropagators and
// Interceptors they run with, so a replay decodes and propagates exactly what
// the live run did.
type ReplayerOptions = worker.WorkflowReplayerOptions

// Workflows is what a manager package's TestMain hands MainWithWorkflows: how
// to register every workflow its workers run, and the replayer options that
// match those workers.
type Workflows struct {
	// Register registers the package's workflows on w exactly as the
	// composition root registers them on a live worker. It must register at
	// least one workflow; a panic (an unfilled hooks stub) is an error.
	Register func(w Worker)
	// Options are the replayer's options (zero value: the SDK defaults).
	Options ReplayerOptions
}

// settleGap and settleTries bound the wait for the dev server's visibility
// store: a listing is taken again settleGap later until two consecutive
// listings name the same executions, at most settleTries times.
const (
	settleGap   = 100 * time.Millisecond
	settleTries = 20
)

// execKey is one workflow execution: a history.
type execKey struct{ workflowID, runID string }

func (k execKey) String() string { return k.workflowID + "/" + k.runID }

// window is the wall-clock span a scenario ran in.
type window struct{ start, end time.Time }

// replayGate is the replayer plus the replay bookkeeping of one Host.
type replayGate struct {
	replayer worker.WorkflowReplayer
	types    map[string]bool // registered workflow type names
	dynamic  bool            // a dynamic workflow is registered: every type is ours

	mu   sync.Mutex
	done map[execKey]bool // settled: closed and replayed, or failed and reported
}

// newReplayGate builds the replayer and runs wf.Register on it. A nil
// Register, a registration that registers no workflow, and one that panics
// are errors.
func newReplayGate(wf Workflows) (gate *replayGate, err error) {
	if wf.Register == nil {
		return nil, errors.New("scenariohost: Workflows.Register is nil")
	}
	replayer, err := worker.NewWorkflowReplayerWithOptions(wf.Options)
	if err != nil {
		return nil, fmt.Errorf("scenariohost: replayer: %w", err)
	}
	gate = &replayGate{replayer: replayer, types: map[string]bool{}, done: map[execKey]bool{}}
	defer func() {
		if p := recover(); p != nil {
			gate, err = nil, fmt.Errorf("scenariohost: registering the package's workflows panicked: %v", p)
		}
	}()
	wf.Register(&replayRegistry{gate: gate})
	if len(gate.types) == 0 && !gate.dynamic {
		return nil, errors.New("scenariohost: Workflows.Register registered no workflow")
	}
	return gate, nil
}

// owns reports whether a history of workflow type name is this package's to
// replay.
func (g *replayGate) owns(name string) bool { return g.dynamic || g.types[name] }

// errReplayRegistry is what Start and Run of the replay registry return.
var errReplayRegistry = errors.New("scenariohost: the replay registry records registrations; it never polls")

// replayRegistry is the Worker a Workflows.Register func registers on.
type replayRegistry struct{ gate *replayGate }

var _ Worker = (*replayRegistry)(nil)

func (r *replayRegistry) RegisterWorkflow(w any) {
	r.gate.replayer.RegisterWorkflow(w)
	r.gate.types[functionName(w)] = true
}

func (r *replayRegistry) RegisterWorkflowWithOptions(w any, options workflow.RegisterOptions) {
	r.gate.replayer.RegisterWorkflowWithOptions(w, options)
	name := options.Name
	if name == "" {
		name = functionName(w)
	}
	r.gate.types[name] = true
}

func (r *replayRegistry) RegisterDynamicWorkflow(w any, options workflow.DynamicRegisterOptions) {
	r.gate.replayer.RegisterDynamicWorkflow(w, options)
	r.gate.dynamic = true
}

func (*replayRegistry) RegisterActivity(any)                                         {}
func (*replayRegistry) RegisterActivityWithOptions(any, activity.RegisterOptions)    {}
func (*replayRegistry) RegisterDynamicActivity(any, activity.DynamicRegisterOptions) {}
func (*replayRegistry) RegisterNexusService(*nexus.Service)                          {}
func (*replayRegistry) Start() error                                                 { return errReplayRegistry }
func (*replayRegistry) Run(<-chan any) error                                         { return errReplayRegistry }
func (*replayRegistry) Stop()                                                        {}

// functionName is the workflow type name the SDK registers a function under
// when no name is given: the function's short name, minus the "-fm" suffix
// the compiler gives a method value.
func functionName(fn any) string {
	if s, ok := fn.(string); ok {
		return s
	}
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	return strings.TrimSuffix(full[strings.LastIndex(full, ".")+1:], "-fm")
}

// replayFailure is one history that did not replay.
type replayFailure struct {
	exec     execKey
	workflow string
	err      error
}

func (f replayFailure) String() string {
	return fmt.Sprintf("replay of workflow history %s (%s) failed: %v", f.exec, f.workflow, f.err)
}

// replayHistories is the replay pass of one Flush: list, fetch, replay,
// attribute. A failure is recorded against the scenario whose window holds
// the execution's start (Flush merges it into that verdict); the returned
// error names every failure this pass found, attributed or not, so each is
// reported once. A host without a gate or a dev server replays nothing.
func (h *Host) replayHistories(ctx context.Context) error {
	g := h.replay
	if g == nil || h.temporal == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	execs, err := settledExecutions(ctx, h.temporal, h.booted)
	if err != nil {
		return fmt.Errorf("scenariohost: replay: list executions: %w", err)
	}
	var msgs []string
	for _, info := range execs {
		if msg := h.replayExecution(ctx, g, info); msg != "" {
			msgs = append(msgs, msg)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	sort.Strings(msgs)
	return fmt.Errorf("scenariohost: %s", strings.Join(msgs, "; "))
}

// replayExecution replays one listed execution this package owns and has not
// settled, returning the failure message ("" when it replayed clean or is
// not this pass's to replay). A failure is settled at once, so it is
// reported once; a clean execution is settled only once it is closed.
func (h *Host) replayExecution(ctx context.Context, g *replayGate, info *workflowpb.WorkflowExecutionInfo) string {
	k := execKey{info.GetExecution().GetWorkflowId(), info.GetExecution().GetRunId()}
	name := info.GetType().GetName()
	if g.done[k] || !g.owns(name) {
		return ""
	}
	replayed, err := g.replayOne(ctx, h.temporal, k)
	if err == nil {
		if replayed && info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			g.done[k] = true
		}
		return ""
	}
	g.done[k] = true
	f := replayFailure{exec: k, workflow: name, err: err}
	key, ok := h.scenarioAt(info.GetStartTime().AsTime())
	if !ok {
		return "no scenario: " + f.String()
	}
	h.failReplay(key, f.String())
	return key.component + " " + key.scenario + ": " + f.String()
}

// replayOne fetches k's whole history and replays it. replayed is false for a
// history with no completed workflow task yet: it recorded no command, so
// there is nothing to compare (a later flush sees it again).
func (g *replayGate) replayOne(ctx context.Context, c client.Client, k execKey) (replayed bool, err error) {
	hist, err := fetchHistory(ctx, c, k)
	if err != nil {
		return true, fmt.Errorf("fetch history: %w", err)
	}
	if !slices.ContainsFunc(hist.GetEvents(), func(e *historypb.HistoryEvent) bool {
		return e.GetEventType() == enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED
	}) {
		return false, nil
	}
	return true, g.replayer.ReplayWorkflowHistoryWithOptions(quietLogger, hist, worker.ReplayWorkflowHistoryOptions{
		OriginalExecution: workflow.Execution{ID: k.workflowID, RunID: k.runID},
	})
}

// quietLogger keeps the replayer's own logging out of the test output; a
// replay failure is reported through the returned error.
var quietLogger = tlog.NewStructuredLogger(slog.New(slog.DiscardHandler))

func fetchHistory(ctx context.Context, c client.Client, k execKey) (*historypb.History, error) {
	it := c.GetWorkflowHistory(ctx, k.workflowID, k.runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	hist := &historypb.History{}
	for it.HasNext() {
		ev, err := it.Next()
		if err != nil {
			return nil, err
		}
		hist.Events = append(hist.Events, ev)
	}
	return hist, nil
}

// settledExecutions lists every execution started at or after since, again
// and again settleGap apart, until two consecutive listings name the same
// executions (the visibility store trails the history store).
func settledExecutions(ctx context.Context, c client.Client, since time.Time) ([]*workflowpb.WorkflowExecutionInfo, error) {
	prev, err := listExecutions(ctx, c, since)
	if err != nil {
		return nil, err
	}
	for range settleTries {
		time.Sleep(settleGap)
		cur, err := listExecutions(ctx, c, since)
		if err != nil {
			return nil, err
		}
		if sameExecutions(prev, cur) {
			return cur, nil
		}
		prev = cur
	}
	return prev, nil
}

func listExecutions(ctx context.Context, c client.Client, since time.Time) ([]*workflowpb.WorkflowExecutionInfo, error) {
	req := &workflowservice.ListWorkflowExecutionsRequest{
		Query: fmt.Sprintf("StartTime >= %q", since.UTC().Format(time.RFC3339Nano)),
	}
	var out []*workflowpb.WorkflowExecutionInfo
	for {
		resp, err := c.ListWorkflow(ctx, req)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.GetExecutions()...)
		if len(resp.GetNextPageToken()) == 0 {
			return out, nil
		}
		req.NextPageToken = resp.GetNextPageToken()
	}
}

func sameExecutions(a, b []*workflowpb.WorkflowExecutionInfo) bool {
	keys := func(in []*workflowpb.WorkflowExecutionInfo) []string {
		out := make([]string, 0, len(in))
		for _, e := range in {
			out = append(out, execKey{e.GetExecution().GetWorkflowId(), e.GetExecution().GetRunId()}.String())
		}
		sort.Strings(out)
		return out
	}
	return slices.Equal(keys(a), keys(b))
}
