package temporalgen

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// workerManifestBlock is the genWorkerManifest / genRegisteredWorkflow /
// genRegisteredActivity type surface emitted verbatim: what the hand-written
// manager supplies to RegisterWorker. Codegen cannot know the workflow
// functions or hybrid hand-written activities, so the manifest carries them;
// the invoker options hook and the genActivities dep struct ride along.
const workerManifestBlock = `// genWorkerManifest is what the hand-written manager supplies to
// RegisterWorker: the workflow functions (codegen cannot know them), any
// hybrid hand-written activities, the invoker options hook, and the
// genActivities dep threading.
type genWorkerManifest struct {
	Workflows        []genRegisteredWorkflow
	CustomActivities []genRegisteredActivity
	ActivityOptions  func(activityName string) (workflow.ActivityOptions, bool)
	Activities       genActivities
}

type genRegisteredWorkflow struct {
	Name string
	Fn   any
}

type genRegisteredActivity struct {
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
// generated activity explicitly by registered name, then the manifest's custom
// (hybrid hand-written) activities. Forgetting a generated activity is
// impossible — the loop over resolveRADeps enumerates them all.
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
	b.WriteString(joinImportGroups([][]string{{
		importLine("go.temporal.io/sdk/activity"),
		importLine("go.temporal.io/sdk/worker"),
		importLine("go.temporal.io/sdk/workflow"),
	}}))
	fmt.Fprintf(&b, "// TaskQueue is the manager's Temporal task queue (derived from the contract\n"+
		"// interface name).\nconst TaskQueue = %q\n\n", taskQueue)
	b.WriteString(workerManifestBlock)
	b.WriteString(registerWorkerFunc(acts))

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

// registerWorkerFunc emits RegisterWorker: workflows first, then each generated
// activity bound off a *genActivities and registered by its Temporal name, then
// the manifest's custom activities.
func registerWorkerFunc(acts []workerActivity) string {
	var b strings.Builder
	b.WriteString("// RegisterWorker registers every workflow + generated activity + custom\n")
	b.WriteString("// activity on w. Forgetting a generated activity is impossible; workflows and\n")
	b.WriteString("// hybrids are exactly what the manifest declares.\n")
	b.WriteString("func RegisterWorker(w worker.Worker, mf genWorkerManifest) {\n")
	b.WriteString("\tfor _, wf := range mf.Workflows {\n")
	b.WriteString("\t\tw.RegisterWorkflowWithOptions(wf.Fn, workflow.RegisterOptions{Name: wf.Name})\n")
	b.WriteString("\t}\n")
	b.WriteString("\tacts := &mf.Activities\n")
	for _, a := range acts {
		fmt.Fprintf(&b, "\tw.RegisterActivityWithOptions(acts.%s, activity.RegisterOptions{Name: %q})\n",
			a.method, a.registered)
	}
	b.WriteString("\tfor _, ca := range mf.CustomActivities {\n")
	b.WriteString("\t\tw.RegisterActivityWithOptions(ca.Fn, activity.RegisterOptions{Name: ca.Name})\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return b.String()
}
