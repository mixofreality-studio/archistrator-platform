package testgen

import (
	"bytes"
	"fmt"
	"go/format"
	"path"
	"regexp"
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
// The runs of one Go package are not one process each: the package's generated TestDiscriminate runs a
// list of them (AIARCH_DISCRIMINATE) as sequential subtests of one `go test`, switching the null
// operations in process between them, so the package builds, links and boots its scenario host once per
// phase (testgen -discriminate: the baselines, then the nulled runs). A web scenario is still one
// scenario-host run per process.
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
// NullOp ("<Iface>.<Op>") nulled. A Go scenario names its Package and Test; a web scenario names its
// Playwright project instead (UIDir, UIConfig, UIGrep) and is run through the scenario host
// (go tool webscenarios), with Test the scenario id.
type Discrimination struct {
	Component string // the serviceContracts key the scenario belongs to
	Scenario  string // the scenario id
	Op        string // the stimulus operation
	Package   string // the module-relative Go package holding the test ("" for a web scenario)
	Test      string // the Go test function (the scenario id for a web scenario)
	NullOp    string // the AIARCH_NULL_OPS value
	// Batch is the Go package the run batches into: every run of one package is made as a subtest of the
	// package's generated TestDiscriminate in one `go test` process (D13). "" for a web scenario, which
	// is run through the scenario host one run per process.
	Batch string
	// UIDir is the Playwright project directory relative to the module root, and UIConfig the
	// generated config relative to it; both are "" for a Go scenario. UIGrep selects the scenario's
	// Playwright test by its title.
	UIDir    string
	UIConfig string
	UIGrep   string
}

// Web reports whether the run is a Playwright scenario run through the scenario host.
func (d Discrimination) Web() bool { return d.UIConfig != "" }

// DiscriminateTest is the generated test that runs a package's null-subject list in one process.
const DiscriminateTest = "TestDiscriminate"

// DiscriminateEnv names the JSON file holding the list TestDiscriminate runs: an array of
// {"id": "<test>#<nullOps>", "test": "<TestScenario_...>", "nullOps": "<Iface>.<Op>,..."}.
const DiscriminateEnv = "AIARCH_DISCRIMINATE"

// DiscriminationSkip is a scenario the run cannot execute yet, so it is neither killed nor a survivor.
type DiscriminationSkip struct {
	Component string
	Scenario  string
	Reason    string
}

// Reasons a runnable scenario is not part of the Go run.
const (
	// reasonWebOp is a web client's operation that reaches no single generated Manager operation:
	// the null run of a web scenario nulls the Manager operation in the launched server, so an
	// operation with none, or with several candidates, names nothing to null.
	reasonWebOp = "a web client's operation reaches no single generated Manager operation, so there is nothing to null"
	// reasonClientOp is a machine client's call that reaches no generated Manager operation: the
	// null run of a client scenario nulls the Manager operation in the launched server, so a call
	// with none names nothing to null.
	reasonClientOp = "a machine client's call reaches no generated Manager operation, so there is nothing to null"
)

// nullSubjectFunc is the generated function that returns plan's subject, nulled when asked to.
func nullSubjectFunc(plan componentPlan) string { return "nullSubject" + ifaceName(plan) }

// runnable reports whether the scenario has steps the run can null.
func (bs boundScenario) runnable() bool { return bs.Binding.Skip == nil && len(bs.Steps) > 0 }

// webDiscriminations lists the null-subject runs of a web client's runnable scenarios: per scenario,
// one per distinct operation its steps drive, nulling the Manager operation that operation reaches
// ("<Iface>.<Op>", the name the composed server's applyNullOps matches) in the server the run
// launches, which inherits AIARCH_NULL_OPS from `go tool webscenarios`. An operation that reaches no
// single generated Manager operation names nothing to null, so it is a recorded skip, never a silent one.
func (o *Output) webDiscriminations(cfg Config, web webEnv, plan componentPlan) {
	uiDir := path.Dir(cfg.UITestsDir)
	uiConfig := path.Join(path.Base(cfg.UITestsDir), plan.Key, "playwright.config.gen.ts")
	for _, bs := range plan.Scenarios {
		if bs.runnable() {
			o.webScenarioRuns(web, plan, bs, uiDir, uiConfig)
		}
	}
}

// webScenarioRuns is one runnable web scenario's runs: one per distinct operation its steps drive, in
// step order.
func (o *Output) webScenarioRuns(web webEnv, plan componentPlan, bs boundScenario, uiDir, uiConfig string) {
	var seen []string
	for _, st := range bs.Steps {
		if slices.Contains(seen, st.Op.Name) {
			continue
		}
		seen = append(seen, st.Op.Name)
		route, ok := web.managerOp(st.Op.Name)
		if !ok {
			o.Skipped = append(o.Skipped, DiscriminationSkip{Component: plan.Key, Scenario: bs.Scenario.ID, Reason: reasonWebOp + ": " + st.Op.Name})
			continue
		}
		o.Discriminate = append(o.Discriminate, Discrimination{
			Component: plan.Key, Scenario: bs.Scenario.ID, Op: st.Op.Name,
			Test: bs.Scenario.ID, NullOp: route.Manager + "." + route.Op,
			UIDir: uiDir, UIConfig: uiConfig, UIGrep: webScenarioGrep(bs.Scenario.ID),
		})
	}
}

// ReasonWebUnseated is why a web scenario is not discriminated yet: the web project it drives has not
// been seated (the web client's own integration seats it), so no browser can load the app's SPA.
const ReasonWebUnseated = "web project not yet seated"

// DeferWeb moves every web scenario's runs out of Discriminate into Skipped with ReasonWebUnseated, one
// skip per scenario, and returns how many scenarios it deferred. The caller counts them in its
// summary, so the deferral is visible, never silent. Go scenarios are untouched.
func (o *Output) DeferWeb() int {
	var kept []Discrimination
	deferred := map[string]bool{}
	for _, d := range o.Discriminate {
		if !d.Web() {
			kept = append(kept, d)
			continue
		}
		key := d.Component + "\x00" + d.Scenario
		if deferred[key] {
			continue
		}
		deferred[key] = true
		o.Skipped = append(o.Skipped, DiscriminationSkip{Component: d.Component, Scenario: d.Scenario, Reason: ReasonWebUnseated})
	}
	o.Discriminate = kept
	sortDiscriminations(o)
	return len(deferred)
}

// managerOp is the generated Manager operation a web client's operation of that name drives: the one
// route of that operation name over every Manager. Several Managers with an operation of the name, or
// none, resolve to nothing.
func (w webEnv) managerOp(op string) (restRoute, bool) {
	var found []restRoute
	for _, key := range sortedKeys(w.routes) {
		if r, ok := w.routes[key][op]; ok {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		return restRoute{}, false
	}
	return found[0], true
}

// webScenarioGrep selects one scenario's Playwright test. Playwright matches --grep against the test's
// title path joined with spaces, and a generated title is "<id> — <title>": the id is anchored on the
// left by the space before the title (or the subject's start) and on the right by the " — " separator,
// so x-P1 selects neither y-x-P1 nor x-P10.
func webScenarioGrep(id string) string {
	return "(?:^|\\s)" + regexp.QuoteMeta(id) + " — "
}

// sortDiscriminations puts the run list in a stable order.
func sortDiscriminations(o *Output) {
	slices.SortStableFunc(o.Discriminate, func(a, b Discrimination) int {
		return strings.Compare(a.Package+"\x00"+a.UIConfig+"\x00"+a.Test+"\x00"+a.NullOp, b.Package+"\x00"+b.UIConfig+"\x00"+b.Test+"\x00"+b.NullOp)
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
			Package: g.goPkg, Batch: g.goPkg, Test: g.testName(plan, bs.Scenario.ID),
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
	imps := map[string]string{"encoding/json": "", "os": "", "strings": "", "testing": "", scenarioHostImport: ""}
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
	g.writeDriver(&b, plans, true)
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("testgen: %s: null file does not format: %w\n%s", g.goPkg, err, b.String())
	}
	return src, nil
}

// scenarioTestNames lists the scenario test functions the package's scenarios file declares, sorted.
func (g *goEmit) scenarioTestNames(plans []componentPlan) []string {
	var names []string
	for _, plan := range plans {
		for _, bs := range plan.Scenarios {
			names = append(names, g.testName(plan, bs.Scenario.ID))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// writeDriver writes the in-process driver of the null-subject run (D13): the table of the package's
// scenario tests and TestDiscriminate, which runs a list of (test, null operations) entries as
// sequential subtests of one process. Each entry sets AIARCH_NULL_OPS with t.Setenv, so an app a
// scenario launches inherits it, and, when the package builds its subject in process (setsNullOps),
// the package's nullOps too; both are restored when the entry ends. Without the list variable the
// test skips. The file importing it needs encoding/json and os (and strings when setsNullOps).
func (g *goEmit) writeDriver(b *bytes.Buffer, plans []componentPlan, setsNullOps bool) {
	b.WriteString("// scenarioTests are the package's scenario tests by name, for the null-subject run's in-process driver.\n")
	b.WriteString("var scenarioTests = map[string]func(*testing.T){\n")
	for _, name := range g.scenarioTestNames(plans) {
		fmt.Fprintf(b, "\t%q: %s,\n", name, name)
	}
	b.WriteString("}\n\n")
	b.WriteString("// discriminateEntry is one null-subject run of the list " + DiscriminateEnv + " names.\n")
	b.WriteString("type discriminateEntry struct {\n\tID      string `json:\"id\"`\n\tTest    string `json:\"test\"`\n\tNullOps string `json:\"nullOps\"`\n}\n\n")
	b.WriteString("// " + DiscriminateTest + " runs the null-subject run's list (" + DiscriminateEnv + ": a JSON file of entries) as\n")
	b.WriteString("// sequential subtests in this one process, each with its operations nulled (testgen -discriminate).\n")
	b.WriteString("func " + DiscriminateTest + "(t *testing.T) {\n")
	fmt.Fprintf(b, "\tfile := os.Getenv(%q)\n\tif file == \"\" {\n\t\tt.Skip(%q)\n\t}\n", DiscriminateEnv, DiscriminateEnv+" names no run list")
	b.WriteString("\traw, err := os.ReadFile(file)\n\tif err != nil {\n\t\tt.Fatalf(\"reading the run list: %v\", err)\n\t}\n")
	b.WriteString("\tvar entries []discriminateEntry\n\tif err := json.Unmarshal(raw, &entries); err != nil {\n\t\tt.Fatalf(\"decoding the run list: %v\", err)\n\t}\n")
	b.WriteString("\tfor _, e := range entries {\n\t\tt.Run(e.ID, func(t *testing.T) {\n")
	b.WriteString("\t\t\tscenario, ok := scenarioTests[e.Test]\n\t\t\tif !ok {\n\t\t\t\tt.Skipf(\"no scenario test %s in this package\", e.Test)\n\t\t\t}\n")
	fmt.Fprintf(b, "\t\t\tt.Setenv(%q, e.NullOps)\n", nullOpsEnv)
	if setsNullOps {
		b.WriteString("\t\t\tprev := nullOps\n\t\t\tt.Cleanup(func() { nullOps = prev })\n")
		b.WriteString("\t\t\tnullOps = nil\n\t\t\tif e.NullOps != \"\" {\n\t\t\t\tnullOps = strings.Split(e.NullOps, \",\")\n\t\t\t}\n")
	}
	b.WriteString("\t\t\tscenario(t)\n\t\t})\n\t}\n}\n")
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
