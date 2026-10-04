package testgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// hooksFile emits the once-generated, agent-owned hooks file of one Go
// package: a subject constructor per contract that has a runnable binding, a
// step stub per Hook:true step and a probe stub per probe on another
// component (names: names.go) — every hook symbol the generated file references and nothing
// else, so a fresh hooks file is drift-clean by construction.
func (g *goEmit) hooksFile(plans []componentPlan) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Hooks for %s scenario tests. Generated ONCE by testgen; owned by the construction agent.\n", hooksSubject(plans))
	b.WriteString("//\n// Every func here is a hook the generated scenarios file calls; the drift check\n// (testgen -check) requires exactly this symbol set, so add or remove hooks by\n// changing the test plan, not this file. Fill the bodies.\n\n")
	fmt.Fprintf(&b, "package %s_test\n\n", g.pkg)
	g.writeImports(&b, map[string]string{"testing": "", scenarioHostImport: "", g.cfg.ModulePath + "/" + g.goPkg: ""})
	for _, plan := range plans {
		g.writeHooksFor(&b, plan)
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: hooks file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// hooksSubject names the hooks file's components in its header line.
func hooksSubject(plans []componentPlan) string {
	keys := make([]string, 0, len(plans))
	for _, p := range plans {
		keys = append(keys, p.Key)
	}
	return strings.Join(keys, ", ")
}

// writeHooksFor writes one contract's hooks: nothing when none of its
// bindings run.
func (g *goEmit) writeHooksFor(b *bytes.Buffer, plan componentPlan) {
	if !hasRunnable(plan) {
		return
	}
	fmt.Fprintf(b, `// %s builds the component under test against the host's real downstream
// doubles. FILL: construct via the generated constructor in %s.
func %s(t *testing.T, h *scenariohost.Host) %s {
	t.Helper()
	t.Fatal("FILL %s")
	return nil
}

`, g.subjectName(plan), g.goPkg, g.subjectName(plan), g.subjectType(plan), g.subjectName(plan))
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip != nil {
			continue
		}
		for _, st := range bs.Steps {
			g.writeStepHooks(b, plan, bs, st)
		}
	}
}

func hasRunnable(plan componentPlan) bool {
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			return true
		}
	}
	return false
}

// writeStepHooks writes the step stub of a hooked step and the probe stub
// of each of its cross-component probes.
func (g *goEmit) writeStepHooks(b *bytes.Buffer, plan componentPlan, bs boundScenario, st boundStep) {
	id, seq := bs.Scenario.ID, st.Bind.Seq
	if st.Bind.Hook {
		name := g.stepName(plan, id, seq)
		fmt.Fprintf(b, `// %s — %s, step %d: %s
// Projected stimulus (JSON): %s
// Binding (JSON): %s
// FILL: arrange state so that guards %s hold, call the operation, return its result.
func %s(t *testing.T, h *scenariohost.Host, subject %s, in %s) (any, error) {
	t.Helper()
	t.Fatal("FILL %s")
	return nil, nil
}

`, name, bs.Scenario.Title, seq, st.Op.Name, compactJSON(st.Stim), compactJSON(st.Bind), guardList(bs), name, g.subjectType(plan), g.inputName(plan, id, seq), name)
	}
	for i, pr := range st.Bind.Probes {
		if _, inline := g.inlineProbe(plan, pr); inline {
			continue
		}
		g.writeProbeHook(b, plan, bs, st, i+1, pr)
	}
}

func (g *goEmit) writeProbeHook(b *bytes.Buffer, plan componentPlan, bs boundScenario, st boundStep, n int, pr methodcheck.Probe) {
	name := g.probeName(plan, bs.Scenario.ID, st.Bind.Seq, n)
	fmt.Fprintf(b, `// %s — %s, step %d, probe %d: %s.%s proves %s.
// Probe (JSON): %s
// FILL: call %s.%s with the probe's inputs on the host's stack and return its result.
func %s(t *testing.T, h *scenariohost.Host, subject %s) (any, error) {
	t.Helper()
	t.Fatal("FILL %s")
	return nil, nil
}

`, name, bs.Scenario.Title, st.Bind.Seq, n, pr.Component, pr.Operation, pr.For, compactJSON(pr), pr.Component, pr.Operation, name, g.subjectType(plan), name)
}

func guardList(bs boundScenario) string {
	if len(bs.Scenario.Guards) == 0 {
		return "(none)"
	}
	var gs []string
	for _, gd := range bs.Scenario.Guards {
		gs = append(gs, gd.At+"="+gd.Guard)
	}
	return strings.Join(gs, ", ")
}

// compactJSON is v as a single comment line.
func compactJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return strings.ReplaceAll(string(raw), "\n", " ")
}
