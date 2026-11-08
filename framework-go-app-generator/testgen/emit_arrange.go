package testgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// emitArrange emits the binding's typed setup, in the plan's order, ahead of its first step. Every
// ArrangeCall variant is one of four things a scenario host already does; none is code an agent wrote.
//
// The switch is exhaustive over the sealed ArrangeCall and has no default, so gochecksumtype fails the
// build of a fifth kind nobody emits.
func (g *goEmit) emitArrange(plan componentPlan, bs boundScenario) {
	for i, a := range bs.Binding.Arrange {
		n := i + 1
		label := fmt.Sprintf("%s arrange %d", bs.Scenario.ID, n)
		fmt.Fprintf(&g.body, "\t\t// arrange %d: %s\n", n, a.Kind())
		switch v := a.(type) {
		case methodcheck.ContractCall:
			g.arrangeContractCall(plan, label, n, v)
		case methodcheck.AdvanceClock:
			g.arrangeAdvanceClock(label, v)
		case methodcheck.FailNext:
			fmt.Fprintf(&g.body, "\t\th.FailNext(t, %q, %q, %q)\n", v.Component, v.Operation, v.ErrorCode)
		case methodcheck.SeedResource:
			g.arrangeSeedResource(label, v)
		}
	}
}

// arrangeContractCall calls a contract operation to build up state, as an anonymous caller: the call
// is made on the component the plan names (built by scenariocompose, sharing the subject's collaborators)
// and its outcome is asserted like a step's.
func (g *goEmit) arrangeContractCall(plan componentPlan, label string, n int, v methodcheck.ContractCall) {
	var tgt target
	recv := "subject"
	if sameComponent(plan, v.Component) {
		if g.tools {
			g.fail(fmt.Errorf("testgen: %s: %s: a machine client's arrange calls the components below it, not its own tools", plan.Key, label))
			return
		}
		tgt = g.ownTarget(plan)
	} else {
		var err error
		if tgt, err = g.targetFor(v.Component); err != nil {
			g.fail(err)
			return
		}
		recv = tgt.construct()
	}
	op, ok := operationNamed(tgt.sc, v.Operation)
	if !ok {
		g.fail(fmt.Errorf("testgen: %s: %s: operation %q is not on %s", plan.Key, label, v.Operation, tgt.key))
		return
	}
	_, refs, err := inputsJSON(tgt.sc, op, v.Inputs)
	if err != nil {
		g.fail(err)
		return
	}
	if len(refs) > 0 {
		g.fail(fmt.Errorf("testgen: %s: %s: argument %q reads the result of step %d, which has not run: arrange runs before the first step", plan.Key, label, refs[0].param, refs[0].ref.Step))
		return
	}
	call := directCall{
		recv: recv, alias: tgt.alias, sc: tgt.sc, op: op, inputs: v.Inputs,
		ctx:   g.ctxExpr(hostRecv, tgt.layer, "", "arrange-"+strconv.Itoa(n)),
		inVar: "in", scenario: label,
	}
	g.body.WriteString("\t\t{\n\t\t\tvar aout any\n\t\t\tvar aerr error\n")
	for _, l := range g.directLines(call, func(c string) string { return assignStmt(op, c, "aout", "aerr") }) {
		fmt.Fprintf(&g.body, "\t\t\t%s\n", l)
	}
	fmt.Fprintf(&g.body, "\t\t\texpect(t, %q, 0, aout, aerr, %s)\n\t\t}\n", label, rawString(expectJSON(v.Expect)))
}

// arrangeAdvanceClock moves the host clock forward; the duration is parsed here so a bad one is a
// generation error, never a test that fatals at run time.
func (g *goEmit) arrangeAdvanceClock(label string, v methodcheck.AdvanceClock) {
	d, err := time.ParseDuration(v.Duration)
	if err != nil {
		g.fail(fmt.Errorf("testgen: %s: advanceClock duration %q: %w", label, v.Duration, err))
		return
	}
	g.usesTime = true
	fmt.Fprintf(&g.body, "\t\th.AdvanceClock(t, time.Duration(%d)) // %s\n", int64(d), v.Duration)
}

// arrangeSeedResource loads the fixture into the resource the host boots.
func (g *goEmit) arrangeSeedResource(label string, v methodcheck.SeedResource) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, v.Fixture); err != nil {
		g.fail(fmt.Errorf("testgen: %s: seedResource %q fixture is not JSON: %w", label, v.Resource, err))
		return
	}
	fmt.Fprintf(&g.body, "\t\th.SeedResource(t, %q, json.RawMessage(%s))\n", v.Resource, rawString(compact.String()))
}
