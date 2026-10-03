// Package testgen emits a component's deterministic scenario tests from the
// per-component test plan committed in project.json
// (deterministic-component-testing design §4.1). It is a pure function of the
// document: `.phaseArtifacts.testPlan[<component>]` pairs, by scenario id, with
// scenario.ForComponent over methodcheck.DeriveScenarios — the SAME canonical
// derivation the TP-* methodcheck rules validate the plan against — and each
// pair becomes one black-box test.
//
// Two targets. A Go component (every contract with a goPackage) gets, per Go
// PACKAGE, one generated file <goPackage>/<stereotype>_scenarios.gen_test.go
// (`package <pkg>_test`; one TestScenario_* per binding, a TestMain that runs
// through the scenario host) and one hooks file
// <goPackage>/<stereotype>_hooks_test.go emitted ONCE and then owned by the
// construction agent: the subject constructor, one Step_* per Hook:true step,
// one Probe_* per probe on another component. A client contract (layer
// "client") gets, per component, one Playwright spec per binding under
// <UITestsDir>/<component>/, the generated Playwright config + results
// reporter, and a once-emitted hooks.ts.
//
// Grouping is by goPackage, not by contract: several contracts can share one
// Go package, and one generated file unions every contract's bindings. A
// package hosting ONE contract uses the plain names the spec gives
// (newSubject, Step_<id>_<seq>, Probe_<id>_<seq>_<n>, TestScenario_<id>); a
// package hosting several infixes the contract's interface name
// (newSubject_<Iface>, Step_<Iface>_<id>_<seq>, …) because two contracts in one
// package can bind the SAME scenario id. A package whose bindings are all
// absent still gets its generated file (TestMain only) so the arch gate's
// closed file set holds and the gap stays visible.
//
// The generated file's names are the ones the framework-go/arch
// scenario-tests-only rule admits: "<stereotype>_scenarios.gen_test.go", with
// the ".gen" marker BEFORE the mandatory "_test.go" suffix, since the Go tool
// compiles only *_test.go as tests.
package testgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
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
	HooksOnce map[string][]byte // path → content; written only if absent
	// Warnings are the plan-shaped conditions testgen tolerates but reports:
	// a contract with no goPackage, a binding whose scenario the derivation
	// does not project onto the component, a projected scenario no binding
	// covers. Each is a TP-* finding's domain; testgen only names it.
	Warnings []string
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
	Key       string // the serviceContracts key, e.g. billingManager
	Contract  methodcheck.ServiceContract
	Scenarios []boundScenario
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
	out := Output{Generated: map[string][]byte{}, HooksOnce: map[string][]byte{}}
	if p.PhaseArtifacts == nil {
		return out, nil
	}
	if cfg.UITestsDir == "" {
		cfg.UITestsDir = defaultUITestsDir
	}
	byPackage, err := routePlans(&out, cfg, p, all)
	if err != nil {
		return out, err
	}
	for _, goPkg := range sortedKeys(byPackage) {
		if err := emitGo(&out, cfg, goPkg, byPackage[goPkg]); err != nil {
			return out, err
		}
	}
	return out, nil
}

// routePlans builds every component's plan and routes it: clients are emitted
// as Playwright on the spot, Go contracts are grouped by goPackage for emitGo,
// and a non-client contract without a goPackage is warned about.
func routePlans(out *Output, cfg Config, p methodcheck.Project, all []scenario.Scenario) (map[string][]componentPlan, error) {
	byPackage := map[string][]componentPlan{}
	for _, comp := range sortedKeys(p.PhaseArtifacts.TestPlan) {
		sc, ok := contractFor(p, comp)
		if !ok {
			return nil, fmt.Errorf("testgen: test plan for unknown component %q", comp)
		}
		plan, err := planFor(out, comp, sc, scenario.ForComponent(all, comp), p.PhaseArtifacts.TestPlan[comp])
		if err != nil {
			return nil, err
		}
		switch {
		case isClient(sc):
			emitPlaywright(out, cfg, plan)
		case sc.GoPackage == "":
			out.Warnings = append(out.Warnings, fmt.Sprintf("testgen: %s has no goPackage; no tests generated", comp))
		default:
			byPackage[sc.GoPackage] = append(byPackage[sc.GoPackage], plan)
		}
	}
	return byPackage, nil
}

// contractFor finds comp's service contract under the serviceContracts key,
// or under scenario.Normalize when the plan is keyed by the kebab-case slot-5
// id rather than the camelCase contract key.
func contractFor(p methodcheck.Project, comp string) (methodcheck.ServiceContract, bool) {
	if sc, ok := p.ServiceContracts[comp]; ok {
		return sc, true
	}
	want := scenario.Normalize(comp)
	for _, k := range sortedKeys(p.ServiceContracts) {
		if scenario.Normalize(k) == want {
			return p.ServiceContracts[k], true
		}
	}
	return methodcheck.ServiceContract{}, false
}

// planFor pairs projected scenarios with their bindings by id, sorted by id. A
// binding naming a scenario the projection lacks, and a projected scenario no
// binding covers, are warnings (TP-BOUND's domain); a bound step whose seq no
// stimulus carries is dropped with a warning (TP-STEP's domain).
func planFor(out *Output, comp string, sc methodcheck.ServiceContract, projected []scenario.Scenario, rec methodcheck.TestPlanRecord) (componentPlan, error) {
	plan := componentPlan{Key: comp, Contract: sc}
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

// hookPrefixes are the hook symbol families a generated Go file references
// in its hooks file: the subject constructor(s), the hooked steps and the
// cross-component probes.
var hookPrefixes = []string{"newSubject", "Step_", "Probe_"}

// Drift compares Generated against disk and verifies every hook symbol
// referenced by a generated Go file exists in the on-disk hooks file next to
// it, and that no hook there is orphaned. For a TypeScript hooks file it
// checks presence. Messages are sorted by path; an empty slice means the tree
// is current. Only an unreadable file that exists is an error.
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
	for _, p := range sortedKeys(out.HooksOnce) {
		m, err := hooksDrift(root, p, out)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m...)
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

// hooksDrift checks one hooks file: presence for every kind, and for a Go
// hooks file the referenced-vs-declared hook symbol sets against the generated
// file in the same directory.
func hooksDrift(root, p string, out Output) ([]string, error) {
	full := filepath.Join(root, filepath.FromSlash(p))
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return []string{"missing hooks file " + p}, nil
	} else if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(p, ".go") {
		return nil, nil
	}
	declared, err := declaredHooks(full)
	if err != nil {
		return []string{fmt.Sprintf("unparseable hooks file %s: %v", p, err)}, nil
	}
	referenced, err := referencedHooks(out, path.Dir(p))
	if err != nil {
		return nil, err
	}
	return symbolDrift(p, referenced, declared), nil
}

// symbolDrift names every referenced hook the hooks file lacks and every
// declared hook nothing references.
func symbolDrift(p string, referenced, declared map[string]bool) []string {
	var msgs []string
	for _, name := range sortedKeys(referenced) {
		if !declared[name] {
			msgs = append(msgs, fmt.Sprintf("missing hook %s in %s", name, p))
		}
	}
	for _, name := range sortedKeys(declared) {
		if !referenced[name] {
			msgs = append(msgs, fmt.Sprintf("orphan hook %s in %s", name, p))
		}
	}
	return msgs
}

// declaredHooks parses the on-disk hooks file and returns its top-level hook
// func names.
func declaredHooks(full string) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), full, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Recv == nil && isHookName(fn.Name.Name) {
			names[fn.Name.Name] = true
		}
	}
	return names, nil
}

// referencedHooks parses the generated Go file(s) in dir and returns every
// hook identifier they call.
func referencedHooks(out Output, dir string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, p := range sortedKeys(out.Generated) {
		if path.Dir(p) != dir || !strings.HasSuffix(p, ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, out.Generated[p], parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("testgen: generated %s does not parse: %w", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && isHookName(id.Name) {
				names[id.Name] = true
			}
			return true
		})
	}
	return names, nil
}

func isHookName(name string) bool {
	for _, pre := range hookPrefixes {
		if strings.HasPrefix(name, pre) {
			return true
		}
	}
	return false
}
