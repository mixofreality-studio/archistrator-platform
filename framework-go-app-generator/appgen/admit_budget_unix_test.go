//go:build unix

package appgen

import (
	"os"
	"slices"
	"syscall"
	"testing"
	"time"
)

// workflowTaskDeadline is how long Temporal's Go SDK lets one workflow task run before it kills
// it as a deadlock: go.temporal.io/sdk internal.defaultDeadlockDetectionTimeout (1 s), the value
// of worker.Options.DeadlockDetectionTimeout when it is left unset. No worker of this platform
// or of archistrator sets it, and the SDK does not export the default.
const workflowTaskDeadline = time.Second

// processCPU is the CPU time (user + system, every thread: the garbage collector's included) the
// process has used.
func processCPU(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// The server rail calls AdmitTier1 inside a workflow task (the delivery Manager's admitDesignDraft,
// FPC Task 6.3 / D8), where the SDK's deadlock detector kills a task that runs longer than
// workflowTaskDeadline. The test pins what one call costs against that deadline.
//
// It measures CPU time, not wall time. The check is pure CPU work, so on an idle worker the two
// agree, but `go test ./...` runs the module's packages in parallel and a wall clock then measures
// the neighbours: CI saw the same call take 1.8 s of wall time that costs 0.35 s of CPU on a
// developer machine. The budget is the deadline itself with no further margin, because CPU time
// on the runner is the margin: the runner's cores are the stand-in for the worker's, so the call fitting
// here means it fits there, and a developer machine's faster cores leave the headroom (0.35 s of
// CPU on one, against 1.0 s). The median of three runs over the todomvc fixture (the largest conformance
// project) and over archistrator's own project.
func TestAdmitTier1_UnderWorkflowBudget(t *testing.T) {
	own, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Input{
		"todomvc":      fixtureInput(t, "todomvc", "server", false),
		"archistrator": {Project: own, ModulePath: "github.com/mixofreality-studio/archistrator/server", ModuleDir: "server", Config: DefaultConfig()},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var runs []time.Duration
			for range 3 {
				before := processCPU(t)
				AdmitTier1(in, KindServiceContract)
				runs = append(runs, processCPU(t)-before)
			}
			slices.Sort(runs)
			t.Logf("CPU time of three runs: %v", runs)
			if runs[1] > workflowTaskDeadline {
				t.Fatalf("median %v of CPU time is over the %v workflow-task deadline (runs %v)", runs[1], workflowTaskDeadline, runs)
			}
		})
	}
}
