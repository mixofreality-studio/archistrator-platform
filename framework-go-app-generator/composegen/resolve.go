package composegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// itoa is a tiny alias so the alias-collision fallback reads cleanly.
func itoa(n int) string { return strconv.Itoa(n) }

// resolved is the fully-resolved composition plan the emitter renders: the
// ordered satellites, ResourceAccess bindings, engines, managers, the derived
// Hooks, and the config-field references — everything computed from the model so
// emitMain is pure string assembly.
type resolved struct {
	cfg         Config
	pkgName     string
	serviceName string

	infra       map[string]projectmodel.InfraDecl // by key
	profiles    []string
	hasTemporal bool
	hasOtel     bool

	temporalKey string // infra key of the temporal decl (for cfg field names)
	postgresKey string

	ras      []raBinding
	engines  []engineComp
	managers []managerComp
	webMgrs  []managerComp

	hooks []hookMethod

	// variantHooks are the per-variant args hooks (G3) — <Comp><Variant>Args —
	// collected during RA resolution, in binding/profile order.
	variantHooks []hookMethod
	// variantHookImports are the import paths the variant-arg hook return types
	// reference (e.g. the projectstate package for the catalog/minter ports).
	variantHookImports []string
	// workerGateHooks are the conditional-worker-registration gates (G6b) —
	// Register<Iface>Worker — collected during manager resolution, in manager
	// order.
	workerGateHooks []hookMethod
	// finalizeHooks are the per-binding post-construction seams (B3) —
	// Finalize<Component> — one per optional/optional-dormant binding that is
	// actually constructed, collected during RA resolution in binding order.
	finalizeHooks []hookMethod

	// hookDeps is every distinct unmatched plain manager dep threaded through a
	// typed Hooks method (the resolver seam: func-typed resolvers AND
	// scalar/interface plain deps with no setting/binding link), first-seen
	// order. Names are qualified per owning manager (G5), so hookSeen dedups
	// only within a manager (no cross-manager collapse of same-named deps).
	hookDeps []hookDep
	hookSeen map[string]bool

	pkgAliases   map[string]string // goPackage -> import alias (collision-disambiguated, G1)
	optDormant   map[string]bool   // RA component key -> binding presence is optional-dormant (G6b)
	localVar     map[string]string // component key -> constructed local var name
	consumedKeys map[string]bool   // infra keys consumed by some binding variant

	// webExposedOverride is the Config.WebExposedManagers lookup set
	// (component key -> true), B1. nil means the driver did not set the
	// override, so the System-relationship derivation (isWebExposed) stands;
	// a non-nil (possibly empty) map REPLACES it entirely.
	webExposedOverride map[string]bool
}

// hookDep is one unmatched plain manager dep resolved to a typed Hooks method:
// <Name>() <goType>, with goImport (if any) added to the emitted imports.
type hookDep struct {
	name     string
	goType   string
	goImport string
}

// raBinding is one ResourceAccess binding resolved to its construction: the
// interface it presents, its package, presence, and one arm per declared
// profile. switched marks a profile switch (>1 arm, or an optional binding whose
// missing-profile arms leave it nil); stub marks a required arm-less binding
// built by its no-arg New<Interface>() stub constructor (G4).
type raBinding struct {
	key        string
	varName    string
	iface      string
	alias      string
	importPath string
	presence   string
	arms       []variantArm
	switched   bool
	stub       bool
}

// variantArm is one profile's variant construction for a binding.
type variantArm struct {
	profile      string
	variant      string // the raw binding variant name (e.g. "postgres", "memory") — the ready-log text
	ctor         string
	args         []string
	returnsError bool
}

// engineComp is one engine-layer contract constructed by its pure New<Iface>().
type engineComp struct {
	varName    string
	ctor       string
	iface      string
	alias      string
	importPath string
}

// managerComp is one manager-layer contract constructed by its generated DI
// constructor + registered on its own Temporal Worker.
type managerComp struct {
	key        string
	varName    string
	iface      string
	alias      string
	importPath string
	ctor       string
	ctorArgs   []string
	webExposed bool
	webAlias   string
	webImport  string
	// gated marks a manager whose Worker registration is guarded by a
	// Register<Iface>Worker(cfg) bool hook (G6b) — it has ≥1 optional-dormant
	// component dep, so whether its Worker runs is composition-root policy.
	gated bool
}

// resolve builds the composition plan.
func resolve(m *projectmodel.Model, cfg Config) (*resolved, error) {
	r := &resolved{
		cfg:          cfg,
		pkgName:      cfg.PackageName,
		serviceName:  cfg.ContainerKey,
		infra:        map[string]projectmodel.InfraDecl{},
		pkgAliases:   map[string]string{},
		optDormant:   map[string]bool{},
		localVar:     map[string]string{},
		consumedKeys: map[string]bool{},
		hookSeen:     map[string]bool{},
	}
	if r.serviceName == "" {
		r.serviceName = "server"
	}
	r.webExposedOverride = webExposedManagerSet(cfg.WebExposedManagers)
	r.resolveInfra(m.Deployment)
	r.profiles = sortedProfiles(m.Deployment)
	r.resolveAliases(m)

	if err := r.resolveRAs(m); err != nil {
		return nil, err
	}
	r.resolveEngines(m)
	if err := r.resolveManagers(m); err != nil {
		return nil, err
	}
	r.hooks = deriveHooks(r)
	return r, nil
}

// resolveAliases computes the import alias for every component package the walk
// imports (bound RAs + engines + managers-with-deps), disambiguating base-name
// collisions (G1). Two packages sharing a last segment (e.g.
// internal/engine/billing + internal/manager/billing) would both import as
// "billing" — uncompilable; each colliding member is instead aliased
// <parentSeg><base> ("enginebilling" / "managerbilling", mirroring hand run()),
// with a numeric fallback if that still collides. A unique base keeps its plain
// segment alias. aliasFor reads the result.
func (r *resolved) resolveAliases(m *projectmodel.Model) {
	pkgs := aliasImportPkgs(m)
	baseCount := map[string]int{}
	for _, gp := range pkgs {
		baseCount[pkgAlias(gp)]++
	}
	used := map[string]bool{}
	for _, gp := range pkgs {
		r.pkgAliases[gp] = uniqueAlias(gp, baseCount, used)
	}
}

// aliasImportPkgs is the deterministic (sorted, de-duplicated) set of component
// goPackages the walk imports: every bound RA + every engine + every
// manager-with-deps.
func aliasImportPkgs(m *projectmodel.Model) []string {
	pkgs := map[string]bool{}
	for _, b := range m.Deployment.Bindings {
		if c, ok := m.Contracts[b.Component]; ok && c != nil && c.GoPackage != "" {
			pkgs[c.GoPackage] = true
		}
	}
	for _, c := range m.Contracts {
		if c != nil && c.GoPackage != "" && importsAsComponent(c) {
			pkgs[c.GoPackage] = true
		}
	}
	out := make([]string, 0, len(pkgs))
	for gp := range pkgs {
		out = append(out, gp)
	}
	sort.Strings(out)
	return out
}

// importsAsComponent reports whether a contract is emitted (and so imported) as a
// constructed component: every engine, and every manager that declares deps.
func importsAsComponent(c *projectmodel.Contract) bool {
	return strings.EqualFold(c.Layer, "engine") || (strings.EqualFold(c.Layer, "manager") && len(c.Deps) > 0)
}

// uniqueAlias returns the import alias for goPackage: its plain last segment when
// that base is unique, else <parentSeg><base> (with a numeric fallback if that
// still collides). used records claimed aliases across the set.
func uniqueAlias(goPackage string, baseCount map[string]int, used map[string]bool) string {
	base := pkgAlias(goPackage)
	alias := base
	if baseCount[base] > 1 {
		alias = parentSeg(goPackage) + base
	}
	for n := 2; used[alias]; n++ {
		alias = base + itoa(n)
	}
	used[alias] = true
	return alias
}

// aliasFor returns the collision-disambiguated import alias for a goPackage
// (falling back to its last segment for a package not in the precomputed set).
func (r *resolved) aliasFor(goPackage string) string {
	if a, ok := r.pkgAliases[goPackage]; ok {
		return a
	}
	return pkgAlias(goPackage)
}

// resolveInfra indexes the infra decls by key and records which root-satellite
// substrates are present.
func (r *resolved) resolveInfra(d *projectmodel.Deployment) {
	for _, decl := range d.Infrastructure {
		r.infra[decl.Key] = decl
		switch decl.Substrate {
		case "temporal":
			r.hasTemporal = true
			r.temporalKey = decl.Key
		case "postgres":
			r.postgresKey = decl.Key
		case "otel":
			r.hasOtel = true
		}
	}
}

// resolveRAs resolves every deployment binding to its RA construction,
// registering each RA's local var so manager component deps resolve against it.
// A binding whose component contract has no goPackage is skipped (G2 — a
// design-only/unbuilt contract carries no DI constructor). An arm-less binding
// is either a required no-arg stub (G4) or, when optional, a literal nil the
// managers receive directly.
func (r *resolved) resolveRAs(m *projectmodel.Model) error {
	binds := append([]projectmodel.Binding(nil), m.Deployment.Bindings...)
	sort.Slice(binds, func(i, j int) bool { return binds[i].Component < binds[j].Component })
	for _, b := range binds {
		if b.Presence == "optional-dormant" {
			r.optDormant[b.Component] = true
		}
		if err := r.resolveOneRA(m, b); err != nil {
			return err
		}
	}
	return nil
}

// resolveOneRA resolves and records one deployment binding (the per-binding
// body of resolveRAs, split out to keep both under the cognitive-complexity
// gate). A binding whose component contract has no goPackage is skipped (G2),
// as is one that stays unbuilt (finalizeBinding — an arm-less optional
// binding).
func (r *resolved) resolveOneRA(m *projectmodel.Model, b projectmodel.Binding) error {
	c, ok := m.Contracts[b.Component]
	if !ok || c == nil || c.Doc == nil || c.GoPackage == "" {
		return nil // G2: no built package / no DI ctor — tolerate an unbuilt contract.
	}
	rb, err := r.resolveBinding(m, b, c)
	if err != nil {
		return err
	}
	if !r.finalizeBinding(&rb, b) {
		return nil
	}
	if isOptionalPresence(rb.presence) {
		// B3: every optional/optional-dormant binding that is actually
		// constructed gets a typed post-construction seam — the
		// composition-root policy hook for e.g. archistrator's construction
		// dry-run stub swap-in.
		r.finalizeHooks = append(r.finalizeHooks, finalizeHook(rb))
	}
	r.ras = append(r.ras, rb)
	r.localVar[b.Component] = rb.varName
	return nil
}

// finalizeBinding resolves an arm-less binding (G4) and sets the switch flag,
// reporting whether the binding is constructed. An arm-less optional binding
// stays unbuilt — managers receive literal nil (no import, no construction) and
// it is NOT appended; an arm-less required binding becomes the modelgen no-arg
// New<Interface>() stub built directly (no profile switch).
func (r *resolved) finalizeBinding(rb *raBinding, b projectmodel.Binding) bool {
	if len(rb.arms) == 0 {
		if !strings.EqualFold(b.Presence, "required") {
			r.localVar[b.Component] = "nil"
			return false
		}
		rb.stub = true
		rb.arms = []variantArm{{variant: "stub", ctor: rb.alias + ".New" + rb.iface}}
	}
	rb.switched = !rb.stub && (len(rb.arms) > 1 || (isOptionalPresence(b.Presence) && len(rb.arms) >= 1))
	return true
}

// isOptionalPresence reports whether a binding presence is optional or
// optional-dormant (a switch leaves it nil on any profile with no arm).
func isOptionalPresence(p string) bool {
	return strings.EqualFold(p, "optional") || strings.EqualFold(p, "optional-dormant")
}

// resolveBinding resolves one binding: its interface/package + one variant arm
// per declared profile (profiles sorted for determinism).
func (r *resolved) resolveBinding(m *projectmodel.Model, b projectmodel.Binding, c *projectmodel.Contract) (raBinding, error) {
	rb := raBinding{
		key:        b.Component,
		varName:    projectmodel.LowerFirst(b.Component),
		iface:      c.Doc.Interface.Name,
		alias:      r.aliasFor(c.GoPackage),
		importPath: r.cfg.ModulePath + "/" + c.GoPackage,
		presence:   b.Presence,
	}
	for _, profile := range r.profiles {
		pv, ok := b.PerProfile[profile]
		if !ok {
			continue
		}
		rb.arms = append(rb.arms, r.resolveArm(rb, profile, pv, b.Settings))
	}
	return rb, nil
}

// resolveArm resolves one profile's variant to a New<Variant><Interface> call.
// When the variant is registered in Config.VariantHookArgs (G3), the WHOLE arg
// list is a single spread call hooks.<Comp><Variant>Args(cfg) — the model can't
// supply those args (composition-root ports / typed values), so the emitter
// delegates them to a typed hook and threads no infra/settings. Otherwise the
// positional convention applies: infra values (per the substrate catalog) then
// binding settings; a variant that consumes any infra returns (Interface,
// error), an infra-free variant (memory/dry-run) returns the interface alone.
func (r *resolved) resolveArm(rb raBinding, profile string, pv projectmodel.BindingVariant, settings []projectmodel.Setting) variantArm {
	ctor := rb.alias + ".New" + variantToken(pv.Variant) + rb.iface
	if specs, ok := r.cfg.VariantHookArgs[rb.key+"/"+pv.Variant]; ok {
		for _, ik := range pv.Infra {
			r.consumedKeys[ik] = true // still a consumed substrate (the hook reads its cfg)
		}
		r.addVariantHook(rb, pv.Variant, specs)
		return variantArm{
			profile:      profile,
			variant:      pv.Variant,
			ctor:         ctor,
			args:         []string{"hooks." + variantHookName(rb.key, pv.Variant) + "(cfg)"},
			returnsError: len(pv.Infra) > 0,
		}
	}
	var args []string
	for _, ik := range pv.Infra {
		decl := r.infra[ik]
		r.consumedKeys[ik] = true
		args = append(args, variantArgsForSubstrate(decl.Substrate, ik)...)
	}
	for _, s := range settings {
		args = append(args, "cfg."+settingFieldName(s.Name))
	}
	return variantArm{
		profile:      profile,
		variant:      pv.Variant,
		ctor:         ctor,
		args:         args,
		returnsError: len(pv.Infra) > 0,
	}
}

// resolveEngines constructs every engine-layer contract via its pure
// New<Interface>() DI constructor, in contract-key order.
func (r *resolved) resolveEngines(m *projectmodel.Model) {
	for _, key := range sortedKeys(m.Contracts) {
		c := m.Contracts[key]
		if !strings.EqualFold(c.Layer, "engine") || c.Doc == nil || c.GoPackage == "" {
			continue // G2: skip a design-only engine contract with no built package.
		}
		e := engineComp{
			varName:    projectmodel.LowerFirst(key),
			iface:      c.Doc.Interface.Name,
			alias:      r.aliasFor(c.GoPackage),
			importPath: r.cfg.ModulePath + "/" + c.GoPackage,
		}
		e.ctor = e.alias + ".New" + e.iface
		r.engines = append(r.engines, e)
		r.localVar[key] = e.varName
	}
}

// resolveManagers constructs every manager-layer contract that declares deps
// (i.e. has a generated DI constructor), threading each dep and recording which
// managers are web-exposed (a client-layer relationship targets them). It
// fails fast with a clear error when some manager needs the Temporal
// control-plane client (client.Client) but the deployment declares no
// "temporal" infrastructure — without this check the walk would emit an
// undefined `tc` reference (G7) instead of a Generate-time diagnostic.
func (r *resolved) resolveManagers(m *projectmodel.Model) error {
	if !r.hasTemporal && requiresTemporalClient(m) {
		return fmt.Errorf("composegen: container %q: managers require a %q infrastructure declaration", r.cfg.ContainerKey, "temporal")
	}
	clientIDs := clientComponentIDs(m.System)
	for _, key := range sortedKeys(m.Contracts) {
		c := m.Contracts[key]
		if !strings.EqualFold(c.Layer, "manager") || c.Doc == nil || len(c.Deps) == 0 || c.GoPackage == "" {
			continue // G2: skip an unbuilt manager contract.
		}
		mc, err := r.resolveManager(m, key, c, clientIDs)
		if err != nil {
			return err
		}
		r.managers = append(r.managers, mc)
		if mc.webExposed {
			r.webMgrs = append(r.webMgrs, mc)
		}
	}
	return nil
}

// resolveManager resolves one manager: its DI constructor + threaded args + web
// exposure.
func (r *resolved) resolveManager(m *projectmodel.Model, key string, c *projectmodel.Contract, clientIDs map[string]bool) (managerComp, error) {
	mc := managerComp{
		key:        key,
		varName:    projectmodel.LowerFirst(key),
		iface:      c.Doc.Interface.Name,
		alias:      r.aliasFor(c.GoPackage),
		importPath: r.cfg.ModulePath + "/" + c.GoPackage,
	}
	mc.ctor = mc.alias + ".New" + mc.iface
	for _, dep := range c.Deps {
		arg, err := r.threadDep(m, mc, dep)
		if err != nil {
			return managerComp{}, fmt.Errorf("composegen: manager %q: %w", key, err)
		}
		mc.ctorArgs = append(mc.ctorArgs, arg)
		// G6b: a component dep bound to an optional-dormant RA makes this
		// manager's Worker registration composition-root policy.
		if dep.Component != "" && r.optDormant[dep.Component] {
			mc.gated = true
		}
	}
	if mc.gated {
		r.workerGateHooks = append(r.workerGateHooks, workerGateHook(mc.iface))
	}
	if r.managerIsWebExposed(m.System, key, clientIDs) {
		mc.webExposed = true
		mc.webAlias = webPkgBase(c.GoPackage) + "web"
		mc.webImport = r.cfg.ModulePath + "/internal/client/web/" + webPkgBase(c.GoPackage)
	}
	return mc, nil
}

// managerIsWebExposed decides whether a manager is web-exposed (B1). When the
// driver supplies Config.WebExposedManagers, that set REPLACES the
// System-relationship derivation entirely — a client→manager relationship
// alone no longer forces web exposure (e.g. archistrator's billingManager
// carries a web-client relationship but clientgen deliberately generates no
// web handler package for it). When the driver leaves it nil, the
// relationship-derived isWebExposed stands unchanged.
func (r *resolved) managerIsWebExposed(s *projectmodel.System, key string, clientIDs map[string]bool) bool {
	if r.webExposedOverride != nil {
		return r.webExposedOverride[key]
	}
	return isWebExposed(s, key, clientIDs)
}

// webExposedManagerSet builds the component-key lookup set for
// Config.WebExposedManagers (B1). nil in, nil out — so the caller can tell
// "driver left it to the model" (nil) apart from "driver explicitly configured
// zero web-exposed managers" (non-nil empty map).
func webExposedManagerSet(keys []string) map[string]bool {
	if keys == nil {
		return nil
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// threadDep resolves one manager DI-constructor dependency to its arg
// expression: a COMPONENT dep to its constructed local var; a PLAIN dep to the
// Temporal client (client.Client), a config setting (name match), or — for
// everything else the deployment model has no link for (a func-typed
// resolver, or a scalar/interface plain dep, e.g. the durableExecution client)
// — a typed Hooks method call (see threadHookDep).
func (r *resolved) threadDep(m *projectmodel.Model, mc managerComp, dep projectmodel.Dep) (string, error) {
	if dep.Component != "" {
		v, ok := r.localVar[dep.Component]
		if !ok {
			return "", fmt.Errorf("component dep %q has no constructed value (needs a binding or engine contract)", dep.Component)
		}
		return v, nil
	}
	if isTemporalClient(dep) {
		return "tc", nil
	}
	if field, ok := hasSetting(m.Deployment, dep.Name); ok {
		return "cfg." + field, nil
	}
	return r.threadHookDep(mc, dep), nil
}

// threadHookDep threads an unmatched plain dep through a typed Hooks method —
// the composition-root resolver seam for anything the model cannot express
// (func-typed resolvers e.g. archistrator's repo lookups, AND scalar/interface
// plain deps e.g. repoBase or the construction ports). G5: the hook is named
// per OWNING MANAGER (<Iface><Dep>) and NEVER deduped across managers — two
// design managers each declaring a "repo" dep get distinct hooks — and any bare
// (unqualified) exported type in the dep's goType is qualified with the owning
// manager's package alias (ProjectID -> systemdesign.ProjectID), since the two
// managers' concrete types differ. One hook per distinct dep name per manager.
func (r *resolved) threadHookDep(mc managerComp, dep projectmodel.Dep) string {
	name := mc.iface + upperFirst(dep.Name)
	if !r.hookSeen[name] {
		r.hookSeen[name] = true
		r.hookDeps = append(r.hookDeps, hookDep{
			name:     name,
			goType:   qualifyBareTypes(strings.TrimSpace(dep.GoType), mc.alias),
			goImport: dep.GoImport,
		})
	}
	return "hooks." + name + "()"
}

// isTemporalClient reports whether a plain dep is the Temporal control-plane
// client (client.Client from go.temporal.io/sdk/client) — sourced from the
// dialed satellite, not a hook.
func isTemporalClient(dep projectmodel.Dep) bool {
	return dep.GoType == "client.Client" && dep.GoImport == "go.temporal.io/sdk/client"
}

// requiresTemporalClient reports whether any built manager-layer contract (the
// same G2-tolerant set resolveManagers walks) declares a client.Client plain
// dep — i.e. the composition root needs the dialed Temporal satellite.
func requiresTemporalClient(m *projectmodel.Model) bool {
	for _, c := range m.Contracts {
		if c == nil || !strings.EqualFold(c.Layer, "manager") || c.Doc == nil || len(c.Deps) == 0 || c.GoPackage == "" {
			continue
		}
		for _, dep := range c.Deps {
			if isTemporalClient(dep) {
				return true
			}
		}
	}
	return false
}

// clientComponentIDs is the set of client-layer component ids (kebab) — the
// sources whose relationships mark a Manager web-exposed.
func clientComponentIDs(s *projectmodel.System) map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for _, c := range s.Components {
		if strings.EqualFold(c.Layer, "client") {
			out[c.ID] = true
		}
	}
	return out
}

// isWebExposed reports whether a client-layer component has a relationship
// targeting the manager component key.
func isWebExposed(s *projectmodel.System, key string, clientIDs map[string]bool) bool {
	if s == nil {
		return false
	}
	comp, ok := s.ComponentByContractKey(key)
	if !ok {
		return false
	}
	for _, rel := range s.Relationships {
		if clientIDs[rel.From] && rel.To == comp.ID {
			return true
		}
	}
	return false
}

// sortedProfiles returns the deployment's distinct env profiles, sorted.
func sortedProfiles(d *projectmodel.Deployment) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range d.Environments {
		if !seen[e.Profile] {
			seen[e.Profile] = true
			out = append(out, e.Profile)
		}
	}
	sort.Strings(out)
	return out
}

// sortedKeys returns the contract keys sorted for deterministic emission.
func sortedKeys(cs map[string]*projectmodel.Contract) []string {
	out := make([]string, 0, len(cs))
	for k := range cs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
