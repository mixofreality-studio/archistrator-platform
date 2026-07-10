package composegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

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

	localVar     map[string]string // component key -> constructed local var name
	consumedKeys map[string]bool   // infra keys consumed by some binding variant
}

// raBinding is one ResourceAccess binding resolved to its profile-switched
// construction: the interface it presents, its package, presence, and one arm
// per declared profile.
type raBinding struct {
	key        string
	varName    string
	iface      string
	alias      string
	importPath string
	presence   string
	arms       []variantArm
}

// variantArm is one profile's variant construction for a binding.
type variantArm struct {
	profile      string
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
}

// resolve builds the composition plan.
func resolve(m *projectmodel.Model, cfg Config) (*resolved, error) {
	r := &resolved{
		cfg:          cfg,
		pkgName:      cfg.PackageName,
		serviceName:  cfg.ContainerKey,
		infra:        map[string]projectmodel.InfraDecl{},
		localVar:     map[string]string{},
		consumedKeys: map[string]bool{},
	}
	if r.serviceName == "" {
		r.serviceName = "server"
	}
	r.resolveInfra(m.Deployment)
	r.profiles = sortedProfiles(m.Deployment)

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

// resolveRAs resolves every deployment binding to a profile-switched RA
// construction, registering each RA's local var so manager component deps
// resolve against it.
func (r *resolved) resolveRAs(m *projectmodel.Model) error {
	binds := append([]projectmodel.Binding(nil), m.Deployment.Bindings...)
	sort.Slice(binds, func(i, j int) bool { return binds[i].Component < binds[j].Component })
	for _, b := range binds {
		rb, err := r.resolveBinding(m, b)
		if err != nil {
			return err
		}
		r.ras = append(r.ras, rb)
		r.localVar[b.Component] = rb.varName
	}
	return nil
}

// resolveBinding resolves one binding: its interface/package + one variant arm
// per declared profile (profiles sorted for determinism).
func (r *resolved) resolveBinding(m *projectmodel.Model, b projectmodel.Binding) (raBinding, error) {
	c, ok := m.Contracts[b.Component]
	if !ok || c.Doc == nil {
		return raBinding{}, fmt.Errorf("composegen: binding %q: unknown/undocumented component", b.Component)
	}
	rb := raBinding{
		key:        b.Component,
		varName:    projectmodel.LowerFirst(b.Component),
		iface:      c.Doc.Interface.Name,
		alias:      pkgAlias(c.GoPackage),
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

// resolveArm resolves one profile's variant to a New<Variant><Interface> call:
// positional infra values (per the substrate catalog) then binding settings. A
// variant that consumes any infra returns (Interface, error); an infra-free
// variant (memory/dry-run) returns the interface alone.
func (r *resolved) resolveArm(rb raBinding, profile string, pv projectmodel.BindingVariant, settings []projectmodel.Setting) variantArm {
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
		ctor:         rb.alias + ".New" + pascalToken(pv.Variant) + rb.iface,
		args:         args,
		returnsError: len(pv.Infra) > 0,
	}
}

// resolveEngines constructs every engine-layer contract via its pure
// New<Interface>() DI constructor, in contract-key order.
func (r *resolved) resolveEngines(m *projectmodel.Model) {
	for _, key := range sortedKeys(m.Contracts) {
		c := m.Contracts[key]
		if !strings.EqualFold(c.Layer, "engine") || c.Doc == nil {
			continue
		}
		e := engineComp{
			varName:    projectmodel.LowerFirst(key),
			iface:      c.Doc.Interface.Name,
			alias:      pkgAlias(c.GoPackage),
			importPath: r.cfg.ModulePath + "/" + c.GoPackage,
		}
		e.ctor = e.alias + ".New" + e.iface
		r.engines = append(r.engines, e)
		r.localVar[key] = e.varName
	}
}

// resolveManagers constructs every manager-layer contract that declares deps
// (i.e. has a generated DI constructor), threading each dep and recording which
// managers are web-exposed (a client-layer relationship targets them).
func (r *resolved) resolveManagers(m *projectmodel.Model) error {
	clientIDs := clientComponentIDs(m.System)
	for _, key := range sortedKeys(m.Contracts) {
		c := m.Contracts[key]
		if !strings.EqualFold(c.Layer, "manager") || c.Doc == nil || len(c.Deps) == 0 {
			continue
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
		alias:      pkgAlias(c.GoPackage),
		importPath: r.cfg.ModulePath + "/" + c.GoPackage,
	}
	mc.ctor = mc.alias + ".New" + mc.iface
	for _, dep := range c.Deps {
		arg, err := r.threadDep(m, dep)
		if err != nil {
			return managerComp{}, fmt.Errorf("composegen: manager %q: %w", key, err)
		}
		mc.ctorArgs = append(mc.ctorArgs, arg)
	}
	if isWebExposed(m.System, key, clientIDs) {
		mc.webExposed = true
		mc.webAlias = webPkgBase(c.GoPackage) + "web"
		mc.webImport = r.cfg.ModulePath + "/internal/client/web/" + webPkgBase(c.GoPackage)
	}
	return mc, nil
}

// threadDep resolves one manager DI-constructor dependency to its arg
// expression: a COMPONENT dep to its constructed local var; a PLAIN dep to the
// Temporal client (client.Client), a config setting (name match), a Hooks
// method (a func type — a resolver seam), or nil (an unbound optional).
func (r *resolved) threadDep(m *projectmodel.Model, dep projectmodel.Dep) (string, error) {
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
	if strings.HasPrefix(strings.TrimSpace(dep.GoType), "func(") {
		return "hooks." + upperFirst(dep.Name) + "()", nil
	}
	return "nil", nil
}

// isTemporalClient reports whether a plain dep is the Temporal control-plane
// client (client.Client from go.temporal.io/sdk/client) — sourced from the
// dialed satellite, not a hook.
func isTemporalClient(dep projectmodel.Dep) bool {
	return dep.GoType == "client.Client" && dep.GoImport == "go.temporal.io/sdk/client"
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
