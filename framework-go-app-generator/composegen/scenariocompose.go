package composegen

import (
	"errors"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// ScenarioComposeFile is the module-relative path of the generated test-subject
// constructors. The package is test support, not a Method component: it builds every
// component from the real substrates a scenario test boots, so it imports across all
// layers and sits outside them (the arch gate exempts exactly this generated package).
const ScenarioComposeFile = "internal/scenariocompose/compose.gen.go"

// scenarioPackage is the package clause of ScenarioComposeFile.
const scenarioPackage = "scenariocompose"

// Import paths the generated subject adds to the composition root's set.
const (
	pathScenarioHost = pathFwBase + "/framework-go-scenariohost"
	pathFwManager    = pathFwBase + "/framework-go/manager"
	pathFwEngine     = pathFwBase + "/framework-go/engine"
	pathFwRA         = pathFwBase + "/framework-go/resourceaccess"
)

// scenarioLayerCtx is the call Context a contract's operations take first, by interface
// layer - the same table modelgen spells the interface with.
var scenarioLayerCtx = map[string]struct{ alias, path, typ string }{
	"manager":        {"fwm", pathFwManager, "fwm.Context"},
	"engine":         {"fweng", pathFwEngine, "fweng.Context"},
	"resourceaccess": {"fwra", pathFwRA, "fwra.Context"},
	"utility":        {"fwra", pathFwRA, "fwra.Context"},
}

// scenarioKind is how a component is built in a test.
type scenarioKind int

const (
	scenarioRA      scenarioKind = iota // the deployment binding's chosen variant arm
	scenarioPure                        // New<Iface>(): an Engine or an infrastructure-free Utility
	scenarioStub                        // New<Iface>(): a stub contract no binding names
	scenarioManager                     // New<Iface>(deps...) plus the Manager's embedded Worker
)

// scenarioComp is one component the generated subject constructs.
type scenarioComp struct {
	key, iface, alias, importPath string
	contract                      *projectmodel.Contract
	kind                          scenarioKind
	// ra is the binding and the arm chosen from it (scenarioRA only).
	ra  raBinding
	arm variantArm
}

// scenarioGen accumulates one emission.
type scenarioGen struct {
	m   *projectmodel.Model
	r   *resolved
	imp *importSet

	comps []scenarioComp
	byKey map[string]scenarioComp
	// refused is the Managers left unbuilt, by contract key (see Refusal).
	refused map[string]Refusal

	body                                 strings.Builder
	usesTemporal, usesPostgres, usesTime bool
	usesWorker, usesHTTP                 bool
}

// EmitScenarioCompose renders internal/scenariocompose/compose.gen.go: for every built
// non-client contract a New<Iface>(t, h) that returns the REAL component, built with the same
// constructor calls main.gen.go makes (ctorCall) over the substrates the scenario host boots
// (h.TemporalHostPort, h.PostgresURL), a Null<Iface>(inner, ops...) wrapper that makes the
// named operations return their zero result (the null-subject run), and Workflows(), every
// Manager's workflows for the replay gate. A machine client has no constructor: its scenario runs
// over the wire against the launched server (scenariohost.LaunchDevstack), whose authorization is
// the composition root's own.
//
// m carries the deployment the app composes with (the authored one, else the default - what
// appgen hands Generate). Where main.gen.go asks the app's hand Hooks, a test subject has
// nobody to ask: a variant whose arguments are hook-supplied, a binding on infrastructure
// the scenario host does not provide, and a Manager dependency the model cannot express are
// generation errors that name the component, never a nil or zero value the test would pass
// against.
//
// One of those is a refusal, not an error: a Manager dependency that is composition-root policy
// (a func-typed resolver, a derived string) the model cannot express. The generated subject has
// no hooks to ask, so that Manager is left unbuilt (no constructor, no Null wrapper, no handler
// over it, no Workflows() registration) and named in the returned Refusals; every other
// component is still generated, so a model with a policy dependency regenerates everything it
// can. The caller reports the refusals; none is ever silently dropped.
func EmitScenarioCompose(m *projectmodel.Model, cfg Config) ([]byte, []Refusal, error) {
	if cfg.ModulePath == "" {
		return nil, nil, fmt.Errorf("composegen: scenariocompose: ModulePath is required")
	}
	if m.Deployment == nil {
		return nil, nil, fmt.Errorf("composegen: scenariocompose: model has no deployment")
	}
	if cfg.ContainerKey != "" && !hasContainer(m.Deployment, cfg.ContainerKey) {
		return nil, nil, fmt.Errorf("composegen: scenariocompose: container %q: not found in deployment", cfg.ContainerKey)
	}
	r := newResolved(m, cfg)
	r.assignAliases(scenarioPackages(m))
	if err := r.resolveRAs(m); err != nil {
		return nil, nil, err
	}
	g := &scenarioGen{m: m, r: r, imp: newImportSet(), byKey: map[string]scenarioComp{}, refused: map[string]Refusal{}}
	if err := g.build(); err != nil {
		return nil, nil, err
	}
	src := g.render()
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, nil, wrapFormatErr(err, src)
	}
	return out, g.refusals(), nil
}

// Refusal is a Manager the generated test subject does not build: one of its constructor
// dependencies is composition-root policy the deployment model cannot express.
type Refusal struct {
	// Manager is the contract key; Dependency the constructor dependency (its name, or the
	// refused Manager's key when the refusal cascades through a component dependency).
	Manager, Dependency string
	// Reason says why, and what would make the Manager buildable.
	Reason string
}

func (r Refusal) String() string {
	return fmt.Sprintf("%s/%s: %s", r.Manager, r.Dependency, r.Reason)
}

// policyDepError is the one managerArg failure that is a refusal rather than a model error.
type policyDepError struct{ Refusal }

func (e policyDepError) Error() string {
	return fmt.Sprintf("composegen: scenariocompose: manager %q dependency %q: %s", e.Manager, e.Dependency, e.Reason)
}

// build sequences the emission: classify the components, then render the component constructors
// and the workflow list, in that order. A machine client has no constructor here: its scenario runs
// over the wire against the launched server (Plan 3 Task 3.3).
func (g *scenarioGen) build() error {
	steps := []func() error{g.classify, g.refuseUnbuildable, g.emitComponents, g.emitWorkflows}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// scenarioPackages is the sorted, de-duplicated goPackage set of every built non-client
// contract: the packages the subject imports.
func scenarioPackages(m *projectmodel.Model) []string {
	seen := map[string]bool{}
	for _, c := range m.Contracts {
		if isScenarioContract(c) {
			seen[c.GoPackage] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// isScenarioContract reports a contract that gets a constructor: built (a package and a
// contract document) and not a Client (a Client is the subject's caller, not its subject).
func isScenarioContract(c *projectmodel.Contract) bool {
	return c != nil && c.Doc != nil && c.GoPackage != "" && !strings.EqualFold(c.Layer, "client")
}

// classify decides how each constructible contract is built, in contract-key order.
func (g *scenarioGen) classify() error {
	bindings := map[string]raBinding{}
	for _, rb := range g.r.ras {
		bindings[rb.key] = rb
	}
	seenIface := map[string]string{}
	for _, key := range sortedKeys(g.m.Contracts) {
		c := g.m.Contracts[key]
		if !isScenarioContract(c) {
			continue
		}
		comp, built, err := g.classifyOne(key, c, bindings)
		if err != nil {
			return err
		}
		if !built {
			continue
		}
		if other, dup := seenIface[comp.iface]; dup {
			return fmt.Errorf("composegen: scenariocompose: components %q and %q both define interface %q, so New%s would be declared twice", other, key, comp.iface, comp.iface)
		}
		seenIface[comp.iface] = key
		g.comps = append(g.comps, comp)
		g.byKey[key] = comp
	}
	return nil
}

// refuseUnbuildable leaves unbuilt every Manager whose constructor arguments the model cannot
// spell (see Refusal), then imports the packages of the components that remain. A Manager that
// depends on a refused Manager is itself refused, so the pass runs to a fixed point.
func (g *scenarioGen) refuseUnbuildable() error {
	for {
		changed, err := g.refuseRound()
		if err != nil {
			return err
		}
		if !changed {
			break
		}
	}
	kept := g.comps[:0]
	for _, comp := range g.comps {
		if g.refused[comp.key].Manager != "" {
			continue
		}
		kept = append(kept, comp)
		g.imp.add(comp.importPath, importAliasFor(comp.importPath, comp.alias))
	}
	g.comps = kept
	return nil
}

// refuseRound refuses every not-yet-refused Manager whose arguments cannot be spelled now,
// and reports whether it refused any.
func (g *scenarioGen) refuseRound() (bool, error) {
	changed := false
	for _, comp := range g.comps {
		if comp.kind != scenarioManager || g.refused[comp.key].Manager != "" {
			continue
		}
		ref, ok, err := g.probeManager(comp)
		if err != nil {
			return false, err
		}
		if ok {
			g.refused[comp.key] = ref
			delete(g.byKey, comp.key)
			changed = true
		}
	}
	return changed, nil
}

// probeManager spells a Manager's constructor arguments to learn whether it is buildable. It
// reports a refusal for a policy dependency; any other failure is a model error. Spelling
// records which imports and helpers the file needs, so the probe leaves them as it found them:
// a refused Manager must not leave a stray import behind.
func (g *scenarioGen) probeManager(comp scenarioComp) (Refusal, bool, error) {
	usesTemporal, usesPostgres, usesTime, usesWorker, usesHTTP := g.usesTemporal, g.usesPostgres, g.usesTime, g.usesWorker, g.usesHTTP
	_, err := g.managerArgs(comp, false)
	g.usesTemporal, g.usesPostgres, g.usesTime, g.usesWorker, g.usesHTTP = usesTemporal, usesPostgres, usesTime, usesWorker, usesHTTP
	var pol policyDepError
	switch {
	case err == nil:
		return Refusal{}, false, nil
	case errors.As(err, &pol):
		return pol.Refusal, true, nil
	default:
		return Refusal{}, false, err
	}
}

// refusals is the refused Managers, ordered by manager then dependency.
func (g *scenarioGen) refusals() []Refusal {
	out := make([]Refusal, 0, len(g.refused))
	for _, r := range g.refused {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manager != out[j].Manager {
			return out[i].Manager < out[j].Manager
		}
		return out[i].Dependency < out[j].Dependency
	})
	return out
}

// classifyOne decides how one constructible contract is built; built is false for a
// component the composition root leaves unbuilt too (an optional binding with no variant).
func (g *scenarioGen) classifyOne(key string, c *projectmodel.Contract, bindings map[string]raBinding) (comp scenarioComp, built bool, err error) {
	comp = scenarioComp{
		key: key, iface: c.Doc.Interface.Name, contract: c,
		alias: g.r.aliasFor(c.GoPackage), importPath: g.r.cfg.ModulePath + "/" + c.GoPackage,
	}
	switch rb, bound := bindings[key]; {
	case bound:
		arm, has, armErr := localArm(rb)
		if armErr != nil {
			return comp, false, armErr
		}
		if !has {
			g.r.localVar[key] = "nil" // an optional binding with no local arm is unbuilt, as in main.gen.go
			return comp, false, nil
		}
		comp.kind, comp.ra, comp.arm = scenarioRA, rb, arm
	case g.r.localVar[key] == "nil":
		return comp, false, nil // unbuilt, as in main.gen.go
	case constructedPure(c):
		comp.kind = scenarioPure
	case strings.EqualFold(c.Layer, "manager"):
		comp.kind = scenarioManager
	case c.Stub:
		comp.kind = scenarioStub
	default:
		return comp, false, fmt.Errorf("composegen: scenariocompose: component %q (%s) has no deployment binding, so a test cannot construct it", key, c.Layer)
	}
	return comp, true, nil
}

// importAliasFor is the explicit alias to import importPath under: none when it is the
// path's last segment (the package's own name).
func importAliasFor(importPath, alias string) string {
	if alias == pkgAlias(importPath) {
		return ""
	}
	return alias
}

// scenarioArgs spells constructor arguments for the generated test subject, from the scenario
// host: the shared Temporal client for a temporal infra, the context and shared pool for a
// postgres one, the host's Input for every config-valued infra input, the declared defaults for
// the binding's settings. It records on the generation what it spelled, so the file imports and
// declares exactly what it uses. It has no hooks to ask: a hook-supplied arm is an error.
type scenarioArgs struct {
	r *resolved
	g *scenarioGen
}

func (a scenarioArgs) infra(key string) (projectmodel.InfraDecl, error) { return a.r.infraDecl(key) }

func (a scenarioArgs) temporal() []string {
	a.g.usesTemporal = true
	return []string{"s.temporal()"}
}

func (a scenarioArgs) postgres() []string {
	a.g.usesPostgres = true
	return []string{"s.t.Context()", "s.postgres()"}
}

// input asks the host for the input: the substrate travels as an untyped constant, so the file
// needs no import for the catalog's type.
func (scenarioArgs) input(decl projectmodel.InfraDecl, in projectmodel.SubstrateInput) string {
	return fmt.Sprintf("s.h.Input(s.t, %q, %q, %q)", decl.Key, decl.Substrate, in.Name)
}

func (a scenarioArgs) setting(s projectmodel.Setting) (string, error) {
	lit, err := settingLiteral(s)
	if err != nil {
		return "", err
	}
	a.g.usesTime = a.g.usesTime || strings.HasPrefix(lit, "time.")
	return lit, nil
}

// hooked spells the one hook-supplied arm the model fully determines, an ExternalHTTP binding:
// the declared endpoint URL and a plain client. Every other hooked arm is an error.
func (a scenarioArgs) hooked(arm variantArm) ([]string, error) {
	if arm.externalInfra == "" {
		return nil, fmt.Errorf("component %q (variant %q) takes composition-root arguments (Config.VariantHookArgs), which only the app's own root can supply", arm.component, arm.variant)
	}
	decl, err := a.r.infraDecl(arm.externalInfra)
	if err != nil {
		return nil, err
	}
	a.g.usesHTTP, a.g.usesTime = true, true
	return []string{a.input(decl, projectmodel.SubstrateInput{Name: "URL"}), "&http.Client{Timeout: 30 * time.Second}"}, nil
}

func (a scenarioArgs) kinds(component string) []string {
	return a.r.registrarKinds(component, a.g.refused)
}

// settingLiteral spells a setting's declared default as a Go literal of the type configgen
// gives its config field.
func settingLiteral(s projectmodel.Setting) (string, error) {
	def := strings.TrimSpace(s.Default)
	switch s.Type {
	case "bool":
		return boolLiteral(s, def)
	case "int":
		return intLiteral(s, def)
	case "duration":
		return durationLiteral(s, def)
	default:
		return strconv.Quote(s.Default), nil
	}
}

func boolLiteral(s projectmodel.Setting, def string) (string, error) {
	if def == "" {
		return "false", nil
	}
	v, err := strconv.ParseBool(def)
	if err != nil {
		return "", fmt.Errorf("setting %q: default %q is not a bool", s.Name, s.Default)
	}
	return strconv.FormatBool(v), nil
}

func intLiteral(s projectmodel.Setting, def string) (string, error) {
	if def == "" {
		return "0", nil
	}
	v, err := strconv.Atoi(def)
	if err != nil {
		return "", fmt.Errorf("setting %q: default %q is not an int", s.Name, s.Default)
	}
	return strconv.Itoa(v), nil
}

func durationLiteral(s projectmodel.Setting, def string) (string, error) {
	var d time.Duration
	if def != "" {
		v, err := time.ParseDuration(def)
		if err != nil {
			return "", fmt.Errorf("setting %q: default %q is not a duration", s.Name, s.Default)
		}
		d = v
	}
	return "time.Duration(" + strconv.FormatInt(int64(d), 10) + ")", nil
}

// findSetting is the deployment's top-level setting a plain dependency is named after.
func findSetting(d *projectmodel.Deployment, name string) (projectmodel.Setting, bool) {
	for _, s := range d.Settings {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return projectmodel.Setting{}, false
}

// managerArgs spells a Manager's constructor arguments. A component dependency is the built
// component (zero: a nil, for the replay registration that never calls it); the Temporal
// client is the shared one (zero: nil); a dependency named after a deployment setting is the
// setting's declared default. Anything else is composition-root policy the model cannot
// express, and is an error.
func (g *scenarioGen) managerArgs(comp scenarioComp, zero bool) ([]string, error) {
	var args []string
	for _, dep := range comp.contract.Deps {
		arg, err := g.managerArg(comp, dep, zero)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

// managerArg spells one Manager constructor argument (see managerArgs).
func (g *scenarioGen) managerArg(comp scenarioComp, dep projectmodel.Dep, zero bool) (string, error) {
	switch {
	case dep.Component != "":
		return g.componentDep(comp, dep, zero)
	case isTemporalClient(dep):
		if zero {
			return "nil", nil
		}
		g.usesTemporal = true
		return "s.temporal()", nil
	}
	name := dep.Name
	if dep.Setting != "" {
		name = dep.Setting
	}
	st, ok := findSetting(g.m.Deployment, name)
	if !ok && dep.Setting != "" {
		return "", fmt.Errorf("composegen: scenariocompose: manager %q dependency %q is linked to setting %q, which the deployment does not declare", comp.key, dep.Name, dep.Setting)
	}
	if !ok {
		return "", policyDepError{Refusal{Manager: comp.key, Dependency: dep.Name, Reason: fmt.Sprintf("(%s) is composition-root policy the deployment model cannot express, and a generated test subject has no hooks to ask: declare it as a deployment setting or a component", strings.TrimSpace(dep.GoType))}}
	}
	lit, err := settingLiteral(st)
	if err != nil {
		return "", fmt.Errorf("composegen: scenariocompose: manager %q dependency %q: %w", comp.key, dep.Name, err)
	}
	g.usesTime = g.usesTime || strings.HasPrefix(lit, "time.")
	return lit, nil
}

// componentDep spells one component dependency.
func (g *scenarioGen) componentDep(comp scenarioComp, dep projectmodel.Dep, zero bool) (string, error) {
	if g.r.localVar[dep.Component] == "nil" {
		return "nil", nil // an unbuilt optional binding: the manager receives nil, as in main.gen.go
	}
	if _, gone := g.refused[dep.Component]; gone {
		return "", policyDepError{Refusal{Manager: comp.key, Dependency: dep.Name, Reason: fmt.Sprintf("depends on manager %q, which the generated test subject does not build", dep.Component)}}
	}
	built, ok := g.byKey[dep.Component]
	if !ok {
		return "", fmt.Errorf("composegen: scenariocompose: manager %q dependency %q: component %q has no constructor (needs a binding or an engine contract)", comp.key, dep.Name, dep.Component)
	}
	if zero {
		// A replay executes the workflow code, which calls a pure dependency (an Engine takes no
		// substrate) in process; only what is reached through an activity is never run.
		if built.kind == scenarioPure {
			return ctorCall(built.alias+".New"+built.iface, nil), nil
		}
		return "nil", nil
	}
	return buildFuncName(dep.Component) + "(s)", nil
}

// buildFuncName is the unexported function that builds (and memoizes) one component.
func buildFuncName(key string) string { return "build" + upperFirst(key) }

// emitComponents renders, per component, its build function, its exported constructor and its
// Null wrapper.
func (g *scenarioGen) emitComponents() error {
	for _, comp := range g.comps {
		if err := g.emitBuild(comp); err != nil {
			return err
		}
		g.emitConstructor(comp)
		if err := g.emitNull(comp); err != nil {
			return err
		}
	}
	return nil
}

// emitBuild renders buildX(s): the memoized construction of one component.
func (g *scenarioGen) emitBuild(comp scenarioComp) error {
	typ := comp.alias + "." + comp.iface
	fmt.Fprintf(&g.body, "// %s builds the %s a scenario drives, once per test.\n", buildFuncName(comp.key), comp.iface)
	fmt.Fprintf(&g.body, "func %s(s *subject) %s {\n", buildFuncName(comp.key), typ)
	fmt.Fprintf(&g.body, "\treturn memoize(s, %q, func() %s {\n", comp.key, typ)
	switch comp.kind {
	case scenarioPure, scenarioStub:
		fmt.Fprintf(&g.body, "\t\treturn %s\n", ctorCall(comp.alias+".New"+comp.iface, nil))
	case scenarioRA:
		if err := g.emitRABuild(comp); err != nil {
			return err
		}
	case scenarioManager:
		if err := g.emitManagerBuild(comp); err != nil {
			return err
		}
	}
	g.body.WriteString("\t})\n}\n\n")
	return nil
}

// emitRABuild renders a bound component's construction from its chosen arm.
func (g *scenarioGen) emitRABuild(comp scenarioComp) error {
	if comp.ra.stub {
		fmt.Fprintf(&g.body, "\t\treturn %s\n", ctorCall(comp.arm.ctor, nil))
		return nil
	}
	args, err := ctorArgs(comp.arm, scenarioArgs{r: g.r, g: g})
	if err != nil {
		return fmt.Errorf("composegen: scenariocompose: component %q (profile %q, variant %q): %w", comp.key, comp.arm.profile, comp.arm.variant, err)
	}
	call := ctorCall(comp.arm.ctor, args)
	if !comp.arm.returnsError {
		fmt.Fprintf(&g.body, "\t\treturn %s\n", call)
		return nil
	}
	fmt.Fprintf(&g.body, "\t\tv, err := %s\n", call)
	fmt.Fprintf(&g.body, "\t\tif err != nil {\n\t\t\ts.t.Fatalf(\"scenariocompose: %s: %%v\", err)\n\t\t}\n", comp.key)
	g.body.WriteString("\t\treturn v\n")
	return nil
}

// emitManagerBuild renders a Manager's construction and, when it has dependencies, the
// embedded Worker main.gen.go starts for it: a Manager's workflows run only while a Worker
// polls its task queue.
func (g *scenarioGen) emitManagerBuild(comp scenarioComp) error {
	c := comp.contract
	ctor := comp.alias + ".New" + comp.iface
	if len(c.Deps) == 0 {
		fmt.Fprintf(&g.body, "\t\treturn %s\n", ctorCall(ctor, nil))
		return nil
	}
	args, err := g.managerArgs(comp, false)
	if err != nil {
		return err
	}
	g.usesTemporal, g.usesWorker = true, true
	fmt.Fprintf(&g.body, "\t\tm := %s\n", ctorCall(ctor, args))
	if g.workerGated(comp) {
		g.body.WriteString("\t\t// An optional-dormant dependency has no variant here: the Worker stays unregistered,\n")
		g.body.WriteString("\t\t// as the composition root's Register<Iface>Worker gate would leave it.\n")
		g.body.WriteString("\t\treturn m\n")
		return nil
	}
	fmt.Fprintf(&g.body, "\t\tw := worker.New(s.temporal(), %s.TaskQueue, worker.Options{})\n", comp.alias)
	fmt.Fprintf(&g.body, "\t\t%s.RegisterManagerWorker(w, m)\n", comp.alias)
	fmt.Fprintf(&g.body, "\t\tif err := w.Start(); err != nil {\n\t\t\ts.t.Fatalf(\"scenariocompose: %s worker: %%v\", err)\n\t\t}\n", comp.key)
	g.body.WriteString("\t\ts.t.Cleanup(w.Stop)\n")
	if reg := g.r.cfg.ScheduleRegistrarComponent; reg != "" && dependsOn(c, reg) {
		fmt.Fprintf(&g.body, "\t\tif err := %s.RegisterSchedules(s.t.Context(), %s(s)); err != nil {\n\t\t\ts.t.Fatalf(\"scenariocompose: %s schedules: %%v\", err)\n\t\t}\n", comp.alias, buildFuncName(reg), comp.key)
	}
	g.body.WriteString("\t\treturn m\n")
	return nil
}

// workerGated reports a Manager whose Worker the composition root would not register:
// one with an optional-dormant dependency that has no variant (a nil).
func (g *scenarioGen) workerGated(comp scenarioComp) bool {
	for _, dep := range comp.contract.Deps {
		if dep.Component != "" && g.r.optDormant[dep.Component] && g.r.localVar[dep.Component] == "nil" {
			return true
		}
	}
	return false
}

// dependsOn reports a contract with a component dependency on key.
func dependsOn(c *projectmodel.Contract, key string) bool {
	for _, d := range c.Deps {
		if d.Component == key {
			return true
		}
	}
	return false
}

// emitConstructor renders the exported New<Iface>.
func (g *scenarioGen) emitConstructor(comp scenarioComp) {
	fmt.Fprintf(&g.body, "// New%s builds the real %s the scenario drives: the generated component over the\n", comp.iface, comp.iface)
	g.body.WriteString("// substrates the scenario host booted. A test's constructors share their components.\n")
	fmt.Fprintf(&g.body, "func New%s(t *testing.T, h *scenariohost.Host) %s.%s {\n", comp.iface, comp.alias, comp.iface)
	fmt.Fprintf(&g.body, "\tt.Helper()\n\treturn %s(subjectFor(t, h))\n}\n\n", buildFuncName(comp.key))
}

// emitNull renders the Null<Iface> wrapper: inner for every operation not named, the zero
// result for each one named ("<Iface>.<Op>").
func (g *scenarioGen) emitNull(comp scenarioComp) error {
	iface, typ := comp.iface, comp.alias+"."+comp.iface
	lc, ok := scenarioLayerCtx[strings.ToLower(comp.contract.Doc.Interface.Layer)]
	if !ok {
		lc, ok = scenarioLayerCtx[strings.ToLower(comp.contract.Layer)]
	}
	if !ok {
		return fmt.Errorf("composegen: scenariocompose: %s: no call context for layer %q", comp.key, comp.contract.Layer)
	}
	g.imp.add(lc.path, lc.alias)

	ops := comp.contract.Doc.Interface.Operations
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = strconv.Quote(op.Name)
	}
	fmt.Fprintf(&g.body, "// null%s wraps a %s, answering the operations it was told to with their zero result.\n", iface, iface)
	fmt.Fprintf(&g.body, "type null%s struct {\n\tinner %s\n\tops   map[string]bool\n}\n\n", iface, typ)
	fmt.Fprintf(&g.body, "// Null%s is inner with each operation named in ops (\"%s.<Op>\") replaced by one that returns\n", iface, iface)
	g.body.WriteString("// the zero result and a nil error: the null-subject run proving a scenario can fail. A name\n")
	fmt.Fprintf(&g.body, "// of this interface that it has no operation for panics, so a typo cannot stub nothing.\n")
	fmt.Fprintf(&g.body, "func Null%s(inner %s, ops ...string) %s {\n", iface, typ, typ)
	fmt.Fprintf(&g.body, "\treturn null%s{inner: inner, ops: nullOps(%q, []string{%s}, ops)}\n}\n\n", iface, iface, strings.Join(names, ", "))

	for _, op := range ops {
		if err := g.emitNullMethod(comp, op, lc.typ); err != nil {
			return err
		}
	}
	return nil
}

// emitNullMethod renders one wrapper method.
func (g *scenarioGen) emitNullMethod(comp scenarioComp, op projectmodel.Operation, ctxType string) error {
	params := []string{"rc " + ctxType}
	args := []string{"rc"}
	for i, p := range op.Params {
		if err := g.checkExported(comp, op, p.Schema); err != nil {
			return err
		}
		name := "p" + strconv.Itoa(i)
		params = append(params, name+" "+scenarioGoType(p.Schema, p.Pointer, comp.alias))
		args = append(args, name)
		g.addSchemaImports(p.Schema)
	}
	if err := g.checkExported(comp, op, op.Result); err != nil {
		return err
	}
	g.addSchemaImports(op.Result)
	ret, zero := nullResults(op, comp.alias)
	fmt.Fprintf(&g.body, "func (n null%s) %s(%s)%s {\n", comp.iface, op.Name, strings.Join(params, ", "), ret)
	call := "n.inner." + op.Name + "(" + strings.Join(args, ", ") + ")"
	fmt.Fprintf(&g.body, "\tif !n.ops[%q] {\n", op.Name)
	if ret == "" {
		fmt.Fprintf(&g.body, "\t\t%s\n\t\treturn\n\t}\n", call)
	} else {
		fmt.Fprintf(&g.body, "\t\treturn %s\n\t}\n", call)
	}
	g.body.WriteString(zero)
	g.body.WriteString("}\n\n")
	return nil
}

// nullResults spells an operation's return clause and the body that answers it with the zero
// result and a nil error.
func nullResults(op projectmodel.Operation, alias string) (ret, zeroBody string) {
	switch {
	case op.Result != nil && op.Error:
		t := scenarioGoType(op.Result, false, alias)
		return " (" + t + ", error)", "\tvar zero " + t + "\n\treturn zero, nil\n"
	case op.Result != nil:
		t := scenarioGoType(op.Result, false, alias)
		return " " + t, "\tvar zero " + t + "\n\treturn zero\n"
	case op.Error:
		return " error", "\treturn nil\n"
	}
	return "", ""
}

// scenarioGoType spells a parameter/result type from outside the contract package, as
// modelgen spells it for the generated fakes: projectmodel.GoType, plus the contract-package
// qualification of a bare exported x-go-type (a type hand-written in the contract package,
// e.g. OrderReceipt), which GoType leaves bare because its callers sit inside that package.
func scenarioGoType(n *projectmodel.SchemaNode, ptr bool, alias string) string {
	t := scenarioGoTypeInner(n, alias)
	if ptr {
		return "*" + t
	}
	return t
}

func scenarioGoTypeInner(n *projectmodel.SchemaNode, alias string) string {
	switch {
	case n == nil:
		return projectmodel.GoType(nil, false, alias)
	case n.XGoType != "":
		return qualifyBareXGoType(n, alias)
	case n.RefName() != "":
		return projectmodel.GoType(n, false, alias)
	case n.IsArray():
		return "[]" + scenarioGoTypeInner(n.Items, alias)
	}
	if v := mapValueSchema(n); v != nil {
		return "map[string]" + scenarioGoTypeInner(v, alias)
	}
	return projectmodel.GoType(n, false, alias)
}

// qualifyBareXGoType qualifies a bare exported x-go-type (no package dot, no import) with
// the contract package alias, as modelgen does for the generated fakes.
func qualifyBareXGoType(n *projectmodel.SchemaNode, alias string) string {
	t := n.XGoType
	if n.XGoImport == "" && !strings.Contains(t, ".") && t[0] >= 'A' && t[0] <= 'Z' {
		return alias + "." + t
	}
	return t
}

// mapValueSchema is the value schema of an inline object that is a map (no properties of its
// own, a schema for its other keys), else nil.
func mapValueSchema(n *projectmodel.SchemaNode) *projectmodel.SchemaNode {
	if n.Type.Primary() != "object" || len(n.Properties) > 0 || n.AdditionalProperties == nil {
		return nil
	}
	return n.AdditionalProperties.Schema
}

// addSchemaImports adds the packages a parameter/result type binds (x-go-import).
func (g *scenarioGen) addSchemaImports(n *projectmodel.SchemaNode) {
	for _, p := range projectmodel.GoImports(n) {
		g.imp.add(p, "")
	}
}

// checkExported refuses a $ref to an unexported type: the generated wrapper lives in another
// package and cannot name it.
func (g *scenarioGen) checkExported(comp scenarioComp, op projectmodel.Operation, n *projectmodel.SchemaNode) error {
	if n == nil {
		return nil
	}
	if name := n.RefName(); name != "" && (name[0] < 'A' || name[0] > 'Z') {
		return fmt.Errorf("composegen: scenariocompose: %s.%s refers to unexported type %q, which the generated wrapper cannot name", comp.iface, op.Name, name)
	}
	return g.checkExported(comp, op, n.Items)
}

// emitWorkflows renders Workflows(): the registration of every Manager's workflows the
// composition root registers, over Managers built with nil collaborators (a replay never
// executes an activity, so registration needs only the receiver's methods).
func (g *scenarioGen) emitWorkflows() error {
	g.body.WriteString("// Workflows registers every Manager's workflows exactly as the composition root registers\n")
	g.body.WriteString("// them on a live Worker, for the replay gate (scenariohost.MainWithWorkflows). The Managers\n")
	g.body.WriteString("// are built with nil collaborators: a replay never executes an activity.\n")
	g.body.WriteString("func Workflows() scenariohost.Workflows {\n")
	var regs []string
	for _, comp := range g.comps {
		if comp.kind != scenarioManager || len(comp.contract.Deps) == 0 {
			continue
		}
		args, err := g.managerArgs(comp, true)
		if err != nil {
			return err
		}
		g.usesWorker = true
		regs = append(regs, fmt.Sprintf("\t\t%s.RegisterManagerWorker(w, %s)\n", comp.alias, ctorCall(comp.alias+".New"+comp.iface, args)))
	}
	if len(regs) == 0 {
		g.body.WriteString("\treturn scenariohost.Workflows{Register: func(scenariohost.Worker) {}}\n}\n")
		return nil
	}
	g.body.WriteString("\treturn scenariohost.Workflows{Register: func(w scenariohost.Worker) {\n")
	for _, reg := range regs {
		g.body.WriteString(reg)
	}
	g.body.WriteString("\t}}\n}\n")
	return nil
}

// render assembles the file: header, imports, the shared subject plumbing, then the
// per-component sections.
func (g *scenarioGen) render() string {
	g.imp.add("sync", "")
	g.imp.add("testing", "")
	g.imp.add(pathScenarioHost, "")
	if g.usesTemporal {
		g.imp.add(pathTemporalClient, "")
		g.imp.add(pathTemporalWorkflow, "")
		g.imp.add(pathTemporalInfra, "temporalprop")
	}
	if g.usesWorker {
		g.imp.add(pathTemporalWorker, "")
	}
	if g.usesPostgres {
		g.imp.add(pathPostgresInfra, "postgresinfra")
		g.imp.add(pathPgxPool, "")
	}
	if g.usesHTTP {
		g.imp.add("net/http", "")
	}
	if g.usesTime {
		g.imp.add("time", "")
	}
	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\n// Package scenariocompose builds the real components a generated scenario test drives, from\n")
	b.WriteString("// the same constructors the composition root calls, over the substrates the scenario host\n")
	b.WriteString("// boots. Nothing here is hand-written: a test's subject is never code an agent wrote.\n")
	b.WriteString("package " + scenarioPackage + "\n\n")
	b.WriteString(g.imp.render())
	b.WriteString(g.subjectPlumbing())
	b.WriteString(g.body.String())
	return b.String()
}

// subjectPlumbing is the per-test state: the shared substrate clients and the memo table.
func (g *scenarioGen) subjectPlumbing() string {
	var b strings.Builder
	b.WriteString("// subject is one test's wiring: it dials each substrate at most once and builds each\n")
	b.WriteString("// component at most once, so the constructors a test calls share their collaborators.\n")
	b.WriteString("type subject struct {\n\tt    *testing.T\n\th    *scenariohost.Host\n\tmemo map[string]any\n")
	if g.usesTemporal {
		b.WriteString("\ttc   client.Client\n")
	}
	if g.usesPostgres {
		b.WriteString("\tpool *pgxpool.Pool\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(subjectLookupSource)
	b.WriteString(nullOpsSource)
	if g.usesTemporal {
		b.WriteString(temporalSource)
	}
	if g.usesPostgres {
		b.WriteString(postgresSource)
	}
	return b.String()
}

const subjectLookupSource = `// subjects holds the wiring of each running test, dropped when the test ends.
var subjects sync.Map // *testing.T -> *subject

func subjectFor(t *testing.T, h *scenariohost.Host) *subject {
	t.Helper()
	if v, ok := subjects.Load(t); ok {
		return v.(*subject)
	}
	s := &subject{t: t, h: h, memo: map[string]any{}}
	subjects.Store(t, s)
	t.Cleanup(func() { subjects.Delete(t) })
	return s
}

func memoize[T any](s *subject, key string, build func() T) T {
	if v, ok := s.memo[key].(T); ok {
		return v
	}
	v := build()
	s.memo[key] = v
	return v
}

`

const nullOpsSource = `// nullOps indexes the operations a Null wrapper stubs out. Names carry their interface
// ("<Iface>.<Op>"); another interface's names are not this wrapper's. A name of this
// interface that it has no operation for panics.
func nullOps(iface string, known, ops []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range ops {
		if len(name) <= len(iface) || name[:len(iface)+1] != iface+"." {
			continue
		}
		op := name[len(iface)+1:]
		found := false
		for _, k := range known {
			found = found || k == op
		}
		if !found {
			panic("scenariocompose: " + iface + " has no operation " + op)
		}
		out[op] = true
	}
	return out
}

`

const temporalSource = `// temporal is the Temporal client every Manager and Temporal-backed component shares,
// dialled like the composition root dials it (the principal propagator included).
func (s *subject) temporal() client.Client {
	s.t.Helper()
	if s.tc != nil {
		return s.tc
	}
	tc, err := client.DialContext(s.t.Context(), client.Options{
		HostPort:           s.h.TemporalHostPort,
		Namespace:          s.h.TemporalNamespace,
		ContextPropagators: []workflow.ContextPropagator{temporalprop.NewPrincipalPropagator()},
	})
	if err != nil {
		s.t.Fatalf("scenariocompose: dial temporal: %v", err)
	}
	s.t.Cleanup(tc.Close)
	s.tc = tc
	return tc
}

`

const postgresSource = `// postgres is the pool every Postgres-backed component shares, over the database the scenario's
// state lives in (the launched app's, once the test launched one; see Host.PostgresDSN). The host
// boots Postgres outside -short only; under -short the test is skipped, never run against nothing.
func (s *subject) postgres() *pgxpool.Pool {
	s.t.Helper()
	if s.pool != nil {
		return s.pool
	}
	if s.h.PostgresURL == "" {
		s.t.Skip("scenariocompose: postgres is not booted under -short")
	}
	pool, err := postgresinfra.NewPool(s.t.Context(), s.h.PostgresDSN())
	if err != nil {
		s.t.Fatalf("scenariocompose: open postgres pool: %v", err)
	}
	s.t.Cleanup(pool.Close)
	s.pool = pool
	return pool
}

`
