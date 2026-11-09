package testgen

import (
	"regexp"
	"strings"
	"testing"
)

// emit_probe_test.go: the probe contract of FPC plan 2, Task 2.13c. A probe is a read the generated
// test makes before and after a step; it must never hang the test, and a read that does not return
// is a verdict (not held before the step, a failure after it).

// probeClosure matches the closure that runs one probe, capturing its body.
var probeClosure = regexp.MustCompile(`(?s)run\d+_\d+ := func\(\) \(any, error\) \{\n(.*?)\n\t\t\}\n`)

// layerContextOnHost matches a call context taken from the host itself rather than from the probe's.
var layerContextOnHost = regexp.MustCompile(`\bh\.(Context|PrincipalContext|ManagerContext(As)?|EngineContext(As)?|ResourceAccessContext(As)?)\(`)

// assertProbesBounded fails unless the file has at least one probe closure and every closure opens a
// probe context and takes every call context from it.
func assertProbesBounded(t *testing.T, name string, src []byte) {
	t.Helper()
	closures := probeClosure.FindAllStringSubmatch(string(src), -1)
	if len(closures) == 0 {
		t.Fatalf("%s: no probe closure in\n%s", name, src)
	}
	for _, c := range closures {
		body := c[1]
		if !strings.Contains(body, "pc := h.ProbeContext(t)") {
			t.Errorf("%s: a probe closure does not open a bounded probe context:\n%s", name, body)
		}
		if layerContextOnHost.MatchString(body) {
			t.Errorf("%s: a probe closure calls with an unbounded host context:\n%s", name, body)
		}
		if !strings.Contains(body, "pc.") {
			t.Errorf("%s: a probe closure never uses its probe context:\n%s", name, body)
		}
	}
}

// Every generated probe call, in a component file and in a machine client's, runs under the bounded
// probe context; the step itself and the arrangement keep the host's.
func TestGo_ProbeCallsAreBounded(t *testing.T) {
	out := generate(t)
	assertProbesBounded(t, billingGen, out.Generated[billingGen])
	assertProbesBounded(t, orderGen, out.Generated[orderGen])

	tools := generateShopTools(t)
	assertProbesBounded(t, toolsGen, tools.Generated[toolsGen])

	src := string(out.Generated[billingGen])
	if !strings.Contains(src, "h.ManagerContextAs(t, principal(") && !strings.Contains(src, "h.ManagerContext(t)") {
		t.Errorf("the step's own call must keep the host context:\n%s", src)
	}
	for _, want := range []string{"func probeTimedOut(err error) bool", "func expectProbe(", "probe timed out"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
}

// A probe that does not return before the pre-state read ends counts as not held: the differential
// still applies to the other probes, and a step whose only pre-state read hung is not called vacuous.
func TestDiscriminate_PreProbeTimeoutIsNotHeld(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	generated := generate(t).Generated
	gen, nullFile := generated[billingGen], generated[billingNull]
	// "blocking": GetInvoice hangs until the invoice is paid. Before the step the read times out
	// (not held), after it the read returns paid, so the scenario passes.
	out, err := runGenerated(t, gen, nullFile, "^TestScenario_process_order_P2$", "blocking")
	if err != nil {
		t.Fatalf("a pre-state probe that times out is not held, so the scenario passes: %v\n%s", err, out)
	}
}

// A post-state probe that does not return fails the step, and says why.
func TestGo_PostProbeTimeoutFailsTheStep(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	generated := generate(t).Generated
	gen, nullFile := generated[billingGen], generated[billingNull]
	out, err := runGenerated(t, gen, nullFile, "^TestScenario_process_order_P2$", "hung")
	if err == nil {
		t.Fatalf("a probe that never returns passed the scenario:\n%s", out)
	}
	if !strings.Contains(out, "probe timed out") {
		t.Fatalf("the failure must say the probe timed out:\n%s", out)
	}
	if strings.Contains(out, "vacuous") {
		t.Fatalf("a hung pre-state read is not held, so the step is not vacuous:\n%s", out)
	}
}

// A step with no probe has no pre-state to read: the differential is skipped, and its falsifiability
// comes from the null-subject run (TP-DISCRIMINATE). This is how a pure Engine's binding is proven.
func TestGo_StepWithoutProbesHasNoDifferential(t *testing.T) {
	src := string(generate(t).Generated[orderGen])
	start := strings.Index(src, "func TestScenario_track_order_P1(")
	if start < 0 {
		t.Fatalf("no track-order-P1 scenario in\n%s", src)
	}
	end := strings.Index(src[start:], "\n}\n")
	body := src[start : start+end]
	if strings.Contains(body, "probe ") {
		t.Fatalf("fixture: track-order-P1 must carry no probe:\n%s", body)
	}
	for _, bad := range []string{"differential(", "held(", "ProbeContext"} {
		if strings.Contains(body, bad) {
			t.Errorf("a probe-less step must not carry %q:\n%s", bad, body)
		}
	}
}

// onBehalfOf travels from the plan's principal into the generated principal table.
func TestGo_PrincipalOnBehalfOfIsEmitted(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		plans := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)
		for _, rec := range plans {
			for _, pr := range rec.(map[string]any)["principals"].(map[string]any) {
				pr.(map[string]any)["onBehalfOf"] = "alice"
			}
		}
	})
	src := string(out.Generated[billingGen])
	if !strings.Contains(src, `OnBehalfOf: "alice"`) {
		t.Errorf("the principal table must carry the delegation:\n%s", src)
	}
}

// intents travel with onBehalfOf into the generated principal table, in the plan's order.
func TestGo_PrincipalTableCarriesIntents(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		plans := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)
		for _, rec := range plans {
			for _, pr := range rec.(map[string]any)["principals"].(map[string]any) {
				pr.(map[string]any)["onBehalfOf"] = "alice"
				pr.(map[string]any)["intents"] = []any{"read", "add"}
			}
		}
	})
	src := string(out.Generated[billingGen])
	if !strings.Contains(src, `OnBehalfOf: "alice", Intents: []string{"read", "add"}`) {
		t.Errorf("the principal table must carry the intents:\n%s", src)
	}
}
