package temporalgen

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// workerManifestBlock is the genWorkerManifest / genRegisteredWorkflow type
// surface emitted verbatim: what the hand-written manager supplies to
// RegisterWorker. Codegen cannot know the workflow functions, so the
// manifest carries them; the invoker options hook and the genActivities dep
// struct ride along. There is no hand-registration escape hatch: the
// file-layout standard bans handwritten Temporal activities, so every
// activity is generated.
const workerManifestBlock = `// genWorkerManifest is what the hand-written manager supplies to
// RegisterWorker: the workflow functions (codegen cannot know them), the
// invoker options hook, and the genActivities dep threading.
type genWorkerManifest struct {
	Workflows       []genRegisteredWorkflow
	ActivityOptions func(activityName string) (workflow.ActivityOptions, bool)
	Activities      genActivities
}

type genRegisteredWorkflow struct {
	Name string
	Fn   any
}

`

// workerActivity is one generated activity to register: the genActivities
// method to bind and the Temporal name to register it under.
type workerActivity struct {
	method     string // genActivities method name (e.g. "OrderStateReadOrder")
	registered string // Temporal activity name (e.g. "orderStateAccess.readOrder")
}

// emitWorker generates worker.gen.go: the TaskQueue const, the genWorkerManifest
// type surface, and RegisterWorker, which registers every workflow, then every
// generated activity explicitly by registered name. There is no hand-written
// activity escape hatch — forgetting a generated activity is impossible, since
// the loop over resolveRADeps enumerates them all.
func emitWorker(ec emitContext) ([]byte, error) {
	deps := resolveRADeps(ec)
	taskQueue := TaskQueueName(ec.mgr.Doc.Interface.Name)

	var acts []workerActivity
	for _, rd := range deps {
		for _, op := range rd.ops {
			acts = append(acts, workerActivity{
				method:     rd.field + op.Name,
				registered: ActivityName(rd.component, op.Name),
			})
		}
	}
	sort.Slice(acts, func(i, j int) bool { return acts[i].registered < acts[j].registered })

	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + ec.pkgName + "\n\n")
	sdk := []string{importLine("go.temporal.io/sdk/worker"), importLine("go.temporal.io/sdk/workflow")}
	if len(acts) > 0 {
		sdk = append(sdk, importLine("go.temporal.io/sdk/activity"))
	}
	b.WriteString(joinImportGroups([][]string{sdk}))
	fmt.Fprintf(&b, "// TaskQueue is the manager's Temporal task queue (derived from the contract\n"+
		"// interface name).\nconst TaskQueue = %q\n\n", taskQueue)
	b.WriteString(workerManifestBlock)
	b.WriteString(registerWorkerFunc(acts))
	b.WriteString(registerManagerWorkerFunc(ec.mgr.Doc.Interface.Name))

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

// registerManagerWorkerFunc emits RegisterManagerWorker, the composition root's one entry: it
// takes the Manager as its contract, so the concrete type stays unexported, and reads the
// manifest the hand-written Manager supplies.
func registerManagerWorkerFunc(iface string) string {
	return fmt.Sprintf(`
// workerManifestSource is what the hand-written manager implements to supply its workflows.
type workerManifestSource interface {
	WorkerManifest() genWorkerManifest
}

// RegisterManagerWorker registers the manager built by New%[1]s on w. It panics when the
// manager does not supply a manifest: a boot-time programming error, like a missing workflow.
func RegisterManagerWorker(w worker.Worker, m %[1]s) {
	src, ok := m.(workerManifestSource)
	if !ok {
		panic("RegisterManagerWorker: %[1]s must implement WorkerManifest() genWorkerManifest")
	}
	RegisterWorker(w, src.WorkerManifest())
}
`, iface)
}

// registerWorkerFunc emits RegisterWorker: workflows first, then each generated
// activity bound off a *genActivities and registered by its Temporal name. With no
// activity to register it binds nothing.
func registerWorkerFunc(acts []workerActivity) string {
	var b strings.Builder
	b.WriteString("// RegisterWorker registers every workflow + generated activity on w.\n")
	b.WriteString("// Forgetting a generated activity is impossible; workflows are exactly what\n")
	b.WriteString("// the manifest declares.\n")
	b.WriteString("func RegisterWorker(w worker.Worker, mf genWorkerManifest) {\n")
	b.WriteString("\tfor _, wf := range mf.Workflows {\n")
	b.WriteString("\t\tw.RegisterWorkflowWithOptions(wf.Fn, workflow.RegisterOptions{Name: wf.Name})\n")
	b.WriteString("\t}\n")
	if len(acts) == 0 {
		// A Manager with no activity-bearing dep (only Engines, or none) registers no activity:
		// binding mf.Activities, or importing the activity package, would not compile.
		b.WriteString("}\n")
		return b.String()
	}
	b.WriteString("\tacts := &mf.Activities\n")
	for _, a := range acts {
		fmt.Fprintf(&b, "\tw.RegisterActivityWithOptions(acts.%s, activity.RegisterOptions{Name: %q})\n",
			a.method, a.registered)
	}
	b.WriteString("}\n")
	return b.String()
}
