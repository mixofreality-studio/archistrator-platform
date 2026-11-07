package testgen

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// stepDialect is how a scenario's stimuli reach the subject: a direct Go call (emit_go.go) or a tool or
// route of a machine client (emit_mcp.go). The step shell around it - result variables, probe closures,
// the pre/post differential - is the same for both and lives in emitStep.
type stepDialect interface {
	// stepComment is the comment line above a step.
	stepComment(st boundStep, bs boundScenario) string
	// stepCall is the statements that run the step and leave its outcome in out / errVar.
	stepCall(plan componentPlan, bs boundScenario, st boundStep, out, errVar string) []string
	// probeBody is the statements of the closure that runs probe n of the step and returns (any, error).
	probeBody(plan componentPlan, bs boundScenario, st boundStep, n int, pr methodcheck.Probe) []string
}

// emitStep emits one bound stimulus. Every probe is a closure, so the same code reads the world before the
// step (the differential's pre-state) and after it (the assertion). A step that expects success and has
// probes fails as vacuous when every probe's expectation already held before it ran (TP-FALSIFIABLE): an
// assertion that was true already proves nothing about the step. A probe that reads this step's own result
// cannot be read before it, and an explicit Before expectation on the step replaces the vacuity check for
// the step's first probe with an assertion of the state the author says holds first.
func (g *goEmit) emitStep(d stepDialect, plan componentPlan, bs boundScenario, st boundStep) {
	id, seq := bs.Scenario.ID, st.Bind.Seq
	if st.Bind.Before != nil && len(st.Bind.Probes) == 0 {
		g.fail(fmt.Errorf("testgen: %s scenario %q step %d: before names a pre-state but the step has no probe to read it with", plan.Key, id, seq))
		return
	}
	line := func(format string, args ...any) { fmt.Fprintf(&g.body, "\t\t"+format+"\n", args...) }
	line("// %s", d.stepComment(st, bs))
	line("var out%d any", seq)
	line("var err%d error", seq)
	g.emitProbeClosures(d, plan, bs, st)
	differential := !st.Bind.Expect.ErrorExpected
	pre := g.emitPreState(id, st, differential)
	for _, l := range d.stepCall(plan, bs, st, fmt.Sprintf("out%d", seq), fmt.Sprintf("err%d", seq)) {
		line("%s", l)
	}
	line("expect(t, %q, %d, out%d, err%d, %s)", id, seq, seq, seq, rawString(expectJSON(st.Bind.Expect)))
	for i, pr := range st.Bind.Probes {
		n := i + 1
		line("pout%d_%d, perr%d_%d = run%d_%d()", seq, n, seq, n, seq, n)
		line("expect(t, %q, %d, pout%d_%d, perr%d_%d, %s)", fmt.Sprintf("%s probe %d.%d", id, seq, n), seq, seq, n, seq, n, rawString(expectJSON(pr.Expect)))
	}
	if differential && strings.Join(pre, "") != strings.Repeat("false", len(pre)) {
		line("differential(t, %q, %d, %s)", id, seq, strings.Join(pre, ", "))
	}
}

// emitProbeClosures declares each probe's result variables and the closure that runs it.
func (g *goEmit) emitProbeClosures(d stepDialect, plan componentPlan, bs boundScenario, st boundStep) {
	seq := st.Bind.Seq
	line := func(format string, args ...any) { fmt.Fprintf(&g.body, "\t\t"+format+"\n", args...) }
	for i, pr := range st.Bind.Probes {
		n := i + 1
		line("// probe %d.%d: %s.%s for %s", seq, n, pr.Component, pr.Operation, pr.For)
		line("var pout%d_%d any", seq, n)
		line("var perr%d_%d error", seq, n)
		line("run%d_%d := func() (any, error) {", seq, n)
		for _, l := range d.probeBody(plan, bs, st, n, pr) {
			line("\t%s", l)
		}
		line("}")
	}
}

// emitPreState reads each probe before the step runs and returns, per probe, the expression for whether its
// expectation already held ("false" for a probe that cannot be read yet or whose pre-state the author gave).
func (g *goEmit) emitPreState(id string, st boundStep, differential bool) []string {
	seq := st.Bind.Seq
	line := func(format string, args ...any) { fmt.Fprintf(&g.body, "\t\t"+format+"\n", args...) }
	var pre []string
	for i, pr := range st.Bind.Probes {
		n := i + 1
		label := fmt.Sprintf("%s probe %d.%d", id, seq, n)
		switch {
		case i == 0 && st.Bind.Before != nil:
			line("pout%d_%d, perr%d_%d = run%d_%d()", seq, n, seq, n, seq, n)
			line("expect(t, %q, %d, pout%d_%d, perr%d_%d, %s)", label+" before", seq, seq, n, seq, n, rawString(expectJSON(*st.Bind.Before)))
			pre = append(pre, "false")
		case !differential || readsStep(pr.Inputs, seq):
			pre = append(pre, "false")
		default:
			name := fmt.Sprintf("pre%d_%d", seq, n)
			line("%s := held(%q, %d, run%d_%d, %s)", name, label+" before", seq, seq, n, rawString(expectJSON(pr.Expect)))
			pre = append(pre, name)
		}
	}
	return pre
}
