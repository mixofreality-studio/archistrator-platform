// Package testgen emits a component's deterministic scenario tests from the
// per-component test plan committed in project.json
// (deterministic-component-testing design §4.1). It is a pure function of the
// document: `.phaseArtifacts.testPlan[<component>]` pairs, by scenario id, with
// scenario.ForComponent over methodcheck.DeriveScenarios — the SAME canonical
// derivation the TP-* methodcheck rules validate the plan against — and each
// pair becomes one black-box test.
//
// Two targets. A Go component (every contract with a goPackage) gets, per Go
// PACKAGE, two generated files: <goPackage>/<stereotype>_scenarios.gen_test.go
// (`package <pkg>_test`; one TestScenario_* per binding) and <goPackage>/null.gen_test.go
// (the package's TestMain, which runs through the scenario host, and the
// null-subject switch of testgen -discriminate; emit_null.go). Nothing else is
// emitted there and nothing is agent-owned: the subject is built by the generated scenariocompose package,
// each step runs as the principal the plan records for its actor, $ref
// arguments are threaded from earlier results, the plan's typed arrange calls
// run before the first step, and every step with probes is checked as a
// pre/post differential (a step whose every probe expectation already held
// before it ran fails as vacuous). A client contract (layer "client") is
// driven in its dialect (§12 B1, emit_mcp.go): a WEB client — ops taking the
// one `action` param — gets, per component, one Playwright spec per binding
// under <UITestsDir>/<component>/, the surface's element inventory (the UI
// design's roles and names, from which every locator is derived), the shared
// runtime, a surface check, the generated Playwright config + results
// reporter (emit_playwright.go); an MCP or REST client — one op per generated
// tool or route — gets the Go pair in its goPackage, whose scenarios launch
// the built app and call the tools or routes over the wire as the plan's
// principals. Nothing testgen emits is agent-owned.
//
// Grouping is by goPackage, not by contract: several contracts can share one
// Go package, and one generated file unions every contract's bindings. A
// package hosting ONE contract uses the plain names (input<ID>S<seq>,
// TestScenario_<id>); a package hosting several infixes the contract's
// interface name because two contracts in one package can bind the SAME
// scenario id. Names are lint-clean by construction (names.go, §12 B3). A
// package whose bindings are all absent still gets both files (the scenarios
// file empty, the null file TestMain only) so the arch gate's closed file set
// holds and the gap stays visible.
//
// The generated files' names are the ones the framework-go/arch
// scenario-tests-only rule admits: "<stereotype>_scenarios.gen_test.go" and
// "null.gen_test.go", with
// the ".gen" marker BEFORE the mandatory "_test.go" suffix, since the Go tool
// compiles only *_test.go as tests.
package testgen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

// Config parameterizes Generate.
type Config struct {
	ModulePath string // Go module of the app, e.g. github.com/x/app/server
	UITestsDir string // relative path for Playwright output, default "uitests/generated"
}

// defaultUITestsDir is where Playwright output lands when Config.UITestsDir is
// empty, relative to the same root the Go paths are relative to.
const defaultUITestsDir = "uitests/generated"

// Output is what one Generate pass produced, keyed by slash-separated path
// relative to the consuming module root.
type Output struct {
	Generated map[string][]byte // path → content; always rewritten
	// Warnings are the plan-shaped conditions testgen tolerates but reports:
	// a contract with no goPackage, a binding whose scenario the derivation
	// does not project onto the component, a projected scenario no binding
	// covers. Each is a TP-* finding's domain; testgen only names it.
	Warnings []string
	// Discriminate is every (scenario, stimulus operation) pair the null-subject run executes, sorted by
	// package then test then operation; Skipped is every runnable scenario the run cannot execute yet,
	// with why (emit_null.go).
	Discriminate []Discrimination
	Skipped      []DiscriminationSkip
}

// boundStep is one projected stimulus paired with its binding and the contract
// operation the binding drives (resolved by name from the binding, since the
// derivation leaves Input.Op empty when a call label matches no op).
type boundStep struct {
	Stim scenario.Stimulus
	Bind methodcheck.StepBinding
	Op   methodcheck.ContractOperation
}

// boundScenario is one projected scenario paired with its binding.
type boundScenario struct {
	Scenario scenario.Scenario
	Binding  methodcheck.ScenarioBinding
	Steps    []boundStep
}

// componentPlan is one contract's bound scenarios, sorted by scenario id.
type componentPlan struct {
	Key        string // the serviceContracts key, e.g. billingManager
	Contract   methodcheck.ServiceContract
	Principals map[string]methodcheck.TestPrincipal // the plan's actor identities
	Scenarios  []boundScenario
	// Design is the surface's recorded UI design (a web client's plan only); nil when none is recorded.
	Design *methodcheck.UIDesignRecord
}

// webEnv is what a browser surface's emitter needs from the rest of the document: every contract (to
// resolve an arrange call's or a probe's component) and the REST route of each Manager operation.
type webEnv struct {
	contracts map[string]methodcheck.ServiceContract
	routes    map[string]map[string]restRoute // contract key → operation name → route
	// noSubject is the components the local-profile composition does not build (an optional binding
	// with no local arm): scenariocompose has no constructor for them, so no scenario can be run.
	noSubject map[string]bool
}

// designOf resolves a plan's surface to its UI design: the plan key, then the contract's component.
func designOf(p methodcheck.Project, key string, sc methodcheck.ServiceContract) (methodcheck.UIDesignRecord, bool) {
	if d, ok := methodcheck.FindUIDesign(p, key); ok {
		return d, true
	}
	return methodcheck.FindUIDesign(p, sc.Component)
}

// Generate emits every component's scenario tests from projectJSON. A test plan
// for a component with no service contract is an error (the plan cannot be
// bound to an operation surface); a bound operation absent from the contract is
// an error (no call can be spelled). A contract without a goPackage that is
// not a client is skipped with a warning.
func Generate(projectJSON []byte, cfg Config) (Output, error) {
	p, _, err := methodcheck.DecodeProject(projectJSON)
	if err != nil {
		return Output{}, err
	}
	all, err := methodcheck.DeriveScenarios(p)
	if err != nil {
		return Output{}, err
	}
	out := Output{Generated: map[string][]byte{}}
	if p.PhaseArtifacts == nil {
		return out, nil
	}
	if cfg.UITestsDir == "" {
		cfg.UITestsDir = defaultUITestsDir
	}
	routes, err := managerRoutes(projectJSON)
	if err != nil {
		return out, err
	}
	web, err := newWebEnv(projectJSON, p.ServiceContracts)
	if err != nil {
		return out, err
	}
	byPackage, err := routePlans(&out, cfg, web, p, all)
	if err != nil {
		return out, err
	}
	for _, goPkg := range sortedKeys(byPackage) {
		if err := emitPackage(&out, cfg, goPkg, byPackage[goPkg], routes, p.ServiceContracts); err != nil {
			return out, err
		}
	}
	sortDiscriminations(&out)
	return out, nil
}

// newWebEnv reads the routes and the no-local-subject components the emitters need.
func newWebEnv(projectJSON []byte, contracts map[string]methodcheck.ServiceContract) (webEnv, error) {
	byComponent, err := componentRoutes(projectJSON)
	if err != nil {
		return webEnv{}, err
	}
	web := webEnv{contracts: contracts, routes: byComponent}
	web.noSubject, err = componentsWithoutLocalSubject(projectJSON)
	return web, err
}

// routePlans builds every component's plan and routes it by surface (§12 B1):
// a web client is emitted as Playwright on the spot; an MCP client and every
// layer contract are grouped by goPackage for emitPackage; a Go-hosted
// contract without a goPackage is warned about.
func routePlans(out *Output, cfg Config, web webEnv, p methodcheck.Project, all []scenario.Scenario) (map[string][]componentPlan, error) {
	byPackage := map[string][]componentPlan{}
	for _, comp := range sortedKeys(p.PhaseArtifacts.TestPlan) {
		plan, err := planOf(out, p, all, comp)
		if err != nil {
			return nil, err
		}
		pkg, err := routePlan(out, cfg, web, plan)
		if err != nil {
			return nil, err
		}
		if pkg != "" {
			byPackage[pkg] = append(byPackage[pkg], plan)
		}
	}
	return byPackage, nil
}

// planOf is one component's plan with its surface's UI design.
func planOf(out *Output, p methodcheck.Project, all []scenario.Scenario, comp string) (componentPlan, error) {
	// The plan's key names the generated test, its results directory and the checks
	// that read them, so it must be the contract key itself (methodcheck TP-KEY).
	sc, ok := p.ServiceContracts[comp]
	if !ok {
		return componentPlan{}, fmt.Errorf("testgen: test plan key %q is not a serviceContracts key", comp)
	}
	// Only a Client's test observes what an actor sees: its projection keeps the actor outcome.
	plan, err := planFor(out, comp, sc, scenario.ForComponentAs(all, comp, isClient(sc)), p.PhaseArtifacts.TestPlan[comp])
	if err != nil {
		return plan, err
	}
	if d, ok := designOf(p, comp, sc); ok {
		plan.Design = &d
	}
	return plan, nil
}

// routePlan routes one plan: a web client is emitted as Playwright here; an
// MCP client or a layer contract returns the goPackage it is grouped under,
// or "" with a warning when it names none.
func routePlan(out *Output, cfg Config, web webEnv, plan componentPlan) (string, error) {
	what := plan.Key + " has"
	if web.noSubject[plan.Key] {
		return "", refuseOrWarnNoSubject(out, plan)
	}
	if isClient(plan.Contract) {
		d, err := clientDialect(plan.Key, plan.Contract)
		if err != nil {
			return "", err
		}
		if d == dialectWeb {
			out.skipDiscrimination(plan, reasonWeb)
			return "", emitPlaywright(out, cfg, web, plan)
		}
		what = plan.Key + " is an MCP client with"
	}
	if plan.Contract.GoPackage == "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s no goPackage; no tests generated", what))
	}
	return plan.Contract.GoPackage, nil
}

// refuseOrWarnNoSubject handles a plan whose component the local-profile composition does not
// build. A recorded binding with no subject is a silent coverage drop, so it is a refusal; the only
// way past it is a dated skip on the binding (or a local arm in the deployment model).
func refuseOrWarnNoSubject(out *Output, plan componentPlan) error {
	live := 0
	for _, bs := range plan.Scenarios {
		if bs.Binding.Skip == nil {
			live++
		}
	}
	if live > 0 {
		return fmt.Errorf("testgen: %s records %d unskipped binding(s) but has no local-profile arm in the deployment model, so scenariocompose builds no subject and nothing could run them: declare the local arm, or record a dated skip {reason, until} on each of its bindings", plan.Key, live)
	}
	out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s has no local-profile arm in the deployment model, so scenariocompose builds no subject; every recorded binding is skipped; no tests generated", plan.Key))
	return nil
}

// emitPackage emits one Go package's scenario tests: every contract in it is
// of one layer, and a client package is the MCP dialect's in-process tools
// test or the REST dialect's in-process routes test (emit_mcp.go, emit_rest.go), every other
// layer a direct-call test (emit_go.go).
func emitPackage(out *Output, cfg Config, goPkg string, plans []componentPlan, routes map[string]restRoute, contracts map[string]methodcheck.ServiceContract) error {
	stereotype, err := sharedStereotype(goPkg, plans)
	if err != nil {
		return err
	}
	if isClient(plans[0].Contract) {
		return emitMCP(out, cfg, goPkg, stereotype, plans, routes, contracts)
	}
	return emitGo(out, cfg, goPkg, stereotype, plans, contracts)
}

// planFor pairs projected scenarios with their bindings by id, sorted by id. A
// binding naming a scenario the projection lacks, and a projected scenario no
// binding covers, are warnings (TP-BOUND's domain); a bound step whose seq no
// stimulus carries is dropped with a warning (TP-STEP's domain).
func planFor(out *Output, comp string, sc methodcheck.ServiceContract, projected []scenario.Scenario, rec methodcheck.TestPlanRecord) (componentPlan, error) {
	plan := componentPlan{Key: comp, Contract: sc, Principals: rec.Principals}
	byID := map[string]scenario.Scenario{}
	for _, s := range projected {
		byID[s.ID] = s
	}
	bound := map[string]bool{}
	for _, b := range rec.Bindings {
		s, ok := byID[b.Scenario]
		if !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s binds scenario %q, which the derivation does not project onto it", comp, b.Scenario))
			continue
		}
		bound[b.Scenario] = true
		bs, err := bindSteps(out, comp, sc, s, b)
		if err != nil {
			return plan, err
		}
		plan.Scenarios = append(plan.Scenarios, bs)
	}
	for _, s := range projected {
		if !bound[s.ID] {
			out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s has no binding for scenario %q", comp, s.ID))
		}
	}
	sort.SliceStable(plan.Scenarios, func(i, j int) bool { return plan.Scenarios[i].Scenario.ID < plan.Scenarios[j].Scenario.ID })
	return plan, nil
}

// bindSteps pairs one binding's steps with the scenario's stimuli by seq and
// resolves each step's operation on the contract. A skipped binding carries no
// steps.
func bindSteps(out *Output, comp string, sc methodcheck.ServiceContract, s scenario.Scenario, b methodcheck.ScenarioBinding) (boundScenario, error) {
	bs := boundScenario{Scenario: s, Binding: b}
	if b.Skip != nil {
		return bs, nil
	}
	for _, step := range b.Steps {
		stim, ok := stimulusAt(s, step.Seq)
		if !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s scenario %q binds seq %d, which the projection lacks", comp, s.ID, step.Seq))
			continue
		}
		op, ok := operationNamed(sc, step.Operation)
		if !ok {
			return bs, fmt.Errorf("testgen: %s scenario %q step %d: operation %q is not on the contract", comp, s.ID, step.Seq, step.Operation)
		}
		bs.Steps = append(bs.Steps, boundStep{Stim: stim, Bind: step, Op: op})
	}
	sort.SliceStable(bs.Steps, func(i, j int) bool { return bs.Steps[i].Bind.Seq < bs.Steps[j].Bind.Seq })
	return bs, nil
}

func stimulusAt(s scenario.Scenario, seq int) (scenario.Stimulus, bool) {
	for _, st := range s.Stimuli {
		if st.Seq == seq {
			return st, true
		}
	}
	return scenario.Stimulus{}, false
}

// operationNamed resolves name on the contract, exactly first and then under
// scenario.Normalize (the derivation's own label-to-op fold).
func operationNamed(sc methodcheck.ServiceContract, name string) (methodcheck.ContractOperation, bool) {
	for _, op := range sc.Interface.Operations {
		if op.Name == name {
			return op, true
		}
	}
	for _, op := range sc.Interface.Operations {
		if scenario.Normalize(op.Name) == scenario.Normalize(name) {
			return op, true
		}
	}
	return methodcheck.ContractOperation{}, false
}

// layerKey folds a contract's layer ("Manager", "resourceaccess", …) to the
// lower-case key modelgen's layer table and the arch stereotypes use.
func layerKey(sc methodcheck.ServiceContract) string {
	l := sc.Layer
	if l == "" {
		l = sc.Interface.Layer
	}
	return strings.ToLower(l)
}

func isClient(sc methodcheck.ServiceContract) bool { return layerKey(sc) == "client" }

// stereotypes maps a layer key to the arch.MethodSpec file stereotype the
// generated test files are named with.
var stereotypes = map[string]string{
	"manager":        "manager",
	"engine":         "engine",
	"resourceaccess": "access",
	"client":         "client",
	"utility":        "utility",
}

// stereotypeOf is the file stereotype of a contract's layer; an unknown layer
// falls back to its own key so the file is still named, never dropped.
func stereotypeOf(sc methodcheck.ServiceContract) string {
	if s, ok := stereotypes[layerKey(sc)]; ok {
		return s
	}
	return layerKey(sc)
}

// ident folds a scenario id (or any label) to a Go identifier fragment:
// non-alphanumerics become "_".
func ident(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Drift compares Generated against disk. Messages are sorted by path; an empty
// slice means the tree is current. Only an unreadable file that exists is an
// error. An agent-owned hooks file (a hooks.ts, a Go *_hooks_test.go) is no part
// of the output any more; cmd/testgen reports any that remains as an orphan.
func Drift(root string, out Output) ([]string, error) {
	var msgs []string
	for _, p := range sortedKeys(out.Generated) {
		stale, err := isStale(filepath.Join(root, filepath.FromSlash(p)), out.Generated[p])
		if err != nil {
			return nil, err
		}
		if stale {
			msgs = append(msgs, "stale: "+p)
		}
	}
	return msgs, nil
}

func isStale(full string, want []byte) (bool, error) {
	got, err := os.ReadFile(full) // #nosec G304 -- a generated path under the caller's root
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return string(got) != string(want), nil
}
