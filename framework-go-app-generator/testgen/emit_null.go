package testgen

import (
	"bytes"
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"
)

// Null-subject discrimination (TP-DISCRIMINATE). A scenario that passes against a subject whose
// stimulus operation does nothing proves nothing about the stimulus. For every Go scenario, testgen
// -discriminate re-runs it once per stimulus operation with that operation replaced by the generated
// Null wrapper (composegen's Null<Iface>), which returns the operation's zero result and a nil error;
// a run that still passes is a survivor and an error.
//
// The switch is the package's TestMain, generated into null.gen_test.go beside the scenarios file: it
// reads AIARCH_NULL_OPS (a comma list of "<Iface>.<Op>") and the scenarios construct their subject
// through nullSubject<Iface>, which is the real subject when the variable is unset. Only the named
// operations are nulled, and only on the scenario's own subject: a probe of another component reads
// through that component's real constructor, so a null stimulus leaves the world as it was and the
// probe expectations must then fail.

// nullFileName is the generated file holding a Go scenario package's TestMain and null switch; the
// arch gate's closed file set admits exactly it and <stereotype>_scenarios.gen_test.go.
const nullFileName = "null.gen_test.go"

// nullOpsEnv is the variable carrying the operations to null, for the test process and for the
// booted server alike.
const nullOpsEnv = "AIARCH_NULL_OPS"

// Discrimination is one null-subject run: the scenario's test, in its package, with the operation
// NullOp ("<Iface>.<Op>") nulled.
type Discrimination struct {
	Component string // the serviceContracts key the scenario belongs to
	Scenario  string // the scenario id
	Op        string // the stimulus operation
	Package   string // the module-relative Go package holding the test
	Test      string // the Go test function
	NullOp    string // the AIARCH_NULL_OPS value
}

// DiscriminationSkip is a scenario the run cannot execute yet, so it is neither killed nor a survivor.
type DiscriminationSkip struct {
	Component string
	Scenario  string
	Reason    string
}

// Reasons a runnable scenario is not part of the Go run.
const (
	reasonWeb = "a web scenario runs against the booted server, which the Playwright half wires in plan 3"
	// reasonClientOp is a machine client's call that reaches no generated Manager operation: the
	// null run of a client scenario nulls the Manager operation in the launched server, so a call
	// with none names nothing to null.
	reasonClientOp = "a machine client's call reaches no generated Manager operation, so there is nothing to null"
)

// nullSubjectFunc is the generated function that returns plan's subject, nulled when asked to.
func nullSubjectFunc(plan componentPlan) string { return "nullSubject" + ifaceName(plan) }

// runnable reports whether the scenario has steps the run can null.
func (bs boundScenario) runnable() bool { return bs.Binding.Skip == nil && len(bs.Steps) > 0 }

// skipDiscrimination records every runnable scenario of plan as not executed, with why.
func (o *Output) skipDiscrimination(plan componentPlan, reason string) {
	for _, bs := range plan.Scenarios {
		if bs.runnable() {
			o.Skipped = append(o.Skipped, DiscriminationSkip{Component: plan.Key, Scenario: bs.Scenario.ID, Reason: reason})
		}
	}
}

// sortDiscriminations puts the run list in a stable order.
func sortDiscriminations(o *Output) {
	slices.SortStableFunc(o.Discriminate, func(a, b Discrimination) int {
		return strings.Compare(a.Package+"\x00"+a.Test+"\x00"+a.NullOp, b.Package+"\x00"+b.Test+"\x00"+b.NullOp)
	})
	slices.SortStableFunc(o.Skipped, func(a, b DiscriminationSkip) int {
		return strings.Compare(a.Component+"\x00"+a.Scenario, b.Component+"\x00"+b.Scenario)
	})
}

// discriminations lists the runs of a Go package's runnable scenarios: one per distinct stimulus
// operation of each.
func (g *goEmit) discriminations(plans []componentPlan) []Discrimination {
	var out []Discrimination
	for _, plan := range plans {
		for _, bs := range plan.Scenarios {
			if bs.runnable() {
				out = append(out, g.scenarioRuns(plan, bs)...)
			}
		}
	}
	return out
}

// scenarioRuns is one scenario's runs: one per distinct stimulus operation, in step order.
func (g *goEmit) scenarioRuns(plan componentPlan, bs boundScenario) []Discrimination {
	var out []Discrimination
	var seen []string
	for _, st := range bs.Steps {
		if slices.Contains(seen, st.Op.Name) {
			continue
		}
		seen = append(seen, st.Op.Name)
		out = append(out, Discrimination{
			Component: plan.Key, Scenario: bs.Scenario.ID, Op: st.Op.Name,
			Package: g.goPkg, Test: g.testName(plan, bs.Scenario.ID),
			NullOp: ifaceName(plan) + "." + st.Op.Name,
		})
	}
	return out
}

// nullFile renders null.gen_test.go: the package's TestMain, the parsed AIARCH_NULL_OPS, and one
// nullSubject<Iface> per contract whose scenarios run.
func (g *goEmit) nullFile(plans []componentPlan) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(genHeader + "\n")
	b.WriteString("// The package's TestMain, and the switch of the null-subject run (testgen -discriminate).\n")
	fmt.Fprintf(&b, "\npackage %s_test\n\n", g.pkg)
	imps := map[string]string{"os": "", "strings": "", "testing": "", scenarioHostImport: ""}
	if g.subject {
		imps[g.cfg.ModulePath+"/"+g.goPkg] = ""
		imps[g.cfg.ModulePath+"/"+scenarioComposeDir] = ""
	}
	g.writeImports(&b, imps)
	comps := make([]string, 0, len(plans))
	for _, plan := range plans {
		comps = append(comps, strconv.Quote(plan.Key))
	}
	b.WriteString("// nullOps are the operations the null-subject run replaces: \"<Iface>.<Op>\" entries of " + nullOpsEnv + ".\n")
	b.WriteString("var nullOps []string\n\n")
	b.WriteString("// TestMain reads " + nullOpsEnv + " before any scenario builds its subject.\n")
	b.WriteString("func TestMain(m *testing.M) {\n")
	fmt.Fprintf(&b, "\tif v := os.Getenv(%q); v != \"\" {\n\t\tnullOps = strings.Split(v, \",\")\n\t}\n", nullOpsEnv)
	if g.replays() {
		// A manager's workflows are replayed at every flush (DCT §12 B4): the generated
		// compose package registers them with the host.
		fmt.Fprintf(&b, "\tscenariohost.MainWithWorkflows(m, %s.Workflows(), %s)\n}\n\n", scenarioComposeName, strings.Join(comps, ", "))
	} else {
		fmt.Fprintf(&b, "\tscenariohost.Main(m, %s)\n}\n\n", strings.Join(comps, ", "))
	}
	for _, plan := range plans {
		if !g.runsAny(plan) {
			continue
		}
		iface := ifaceName(plan)
		fmt.Fprintf(&b, "// %s is plan's subject: the real %s, or its Null wrapper when %s names operations of it.\n", nullSubjectFunc(plan), iface, nullOpsEnv)
		fmt.Fprintf(&b, "func %s(inner %s) %s {\n", nullSubjectFunc(plan), g.subjectType(plan), g.subjectType(plan))
		fmt.Fprintf(&b, "\tif len(nullOps) == 0 {\n\t\treturn inner\n\t}\n")
		fmt.Fprintf(&b, "\treturn %s.Null%s(inner, nullOps...)\n}\n\n", scenarioComposeName, iface)
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: null file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// runsAny reports whether any of plan's bindings builds a subject.
func (g *goEmit) runsAny(plan componentPlan) bool {
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			return true
		}
	}
	return false
}
