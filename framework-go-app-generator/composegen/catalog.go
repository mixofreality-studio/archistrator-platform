package composegen

import (
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The substrate catalog (a substrate's ordered configuration inputs) is projectmodel's:
// projectmodel.Substrate.Inputs. configgen emits the fields from it and composegen threads the
// SAME cfg fields, so the two cannot drift.
//
// The substrates that produce a SINGLE root-level satellite the composition
// root constructs once and shares (rather than config values a variant consumes
// directly) are otel/temporal/postgres — their construction is emitted in the
// fixed walk order (telemetry → temporal → postgres) before any binding (see
// resolved.resolveInfra + the write* functions).

// argSource spells the pieces of a constructor call for one of the two places a component is
// built: the composition root (main.gen.go, over its dialed satellites and Config) and the
// generated test subject (scenariocompose, over the substrates the scenario host boots). The
// pieces are the same; only their spelling differs, so ctorArgs is written once over this
// interface and neither root can drift from the other on what a constructor takes.
type argSource interface {
	// infra is the deployment's declaration of one infra key; an undeclared key is an error.
	infra(key string) (projectmodel.InfraDecl, error)
	// temporal and postgres are the shared satellites: the dialed client, and the context and pool.
	temporal() []string
	postgres() []string
	// input is one catalog input of a config-valued infra declaration.
	input(decl projectmodel.InfraDecl, in projectmodel.SubstrateInput) string
	// setting is one binding setting.
	setting(s projectmodel.Setting) (string, error)
	// hooked is the arguments of an arm whose constructor arguments are hook-supplied.
	hooked(arm variantArm) ([]string, error)
	// kinds is what the schedule-registrar component's constructor takes after its other
	// arguments, nil for every other component.
	kinds(component string) []string
}

// ctorArgs is the one place an arm's constructor arguments are spelled: the infra values (per
// the substrate catalog, in the step-8 A1 positional convention), then the binding settings,
// then - for the schedule registrar - the ExecutionKinds of the Managers that depend on it
// (last, because the registrar's constructor takes them variadically). A hook-supplied arm is
// spelled by the source alone.
//
// The switch over substrates (substrateArgs) is exhaustive: projectmodel.Substrates lists every
// kind, and a test holds this to it.
func ctorArgs(arm variantArm, src argSource) ([]string, error) {
	if arm.hooked {
		return src.hooked(arm)
	}
	var args []string
	for _, ik := range arm.infra {
		decl, err := src.infra(ik)
		if err != nil {
			return nil, err
		}
		a, err := substrateArgs(decl, src)
		if err != nil {
			return nil, err
		}
		args = append(args, a...)
	}
	for _, s := range arm.settings {
		v, err := src.setting(s)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	return append(args, src.kinds(arm.component)...), nil
}

// substrateArgs is the arguments one infra declaration contributes to a variant constructor.
//
//   - temporal → the shared dialed client.
//   - postgres → the request context and the shared pool.
//   - otel     → nothing (telemetry is a global install, never a variant dep).
//   - otherwise → one value per catalog input of the declaration (github-app / keycloak /
//     external-http / git-repo: the variant mints its own client internally from these - the A1
//     fold that keeps sibling infra out of the RA surface).
func substrateArgs(decl projectmodel.InfraDecl, src argSource) ([]string, error) {
	switch decl.Substrate {
	case projectmodel.SubstrateTemporal:
		return src.temporal(), nil
	case projectmodel.SubstratePostgres:
		return src.postgres(), nil
	case projectmodel.SubstrateOtel:
		return nil, nil
	case projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateExternalHTTP, projectmodel.SubstrateGitRepo:
		var out []string
		for _, in := range decl.Substrate.Inputs() {
			out = append(out, src.input(decl, in))
		}
		return out, nil
	}
	return nil, fmt.Errorf("infrastructure %q: unknown substrate %q (known: %v)", decl.Key, decl.Substrate, projectmodel.Substrates())
}

// infraDecl is the deployment's declaration of one infra key.
func (r *resolved) infraDecl(key string) (projectmodel.InfraDecl, error) {
	decl, ok := r.infra[key]
	if !ok {
		return decl, fmt.Errorf("infrastructure %q is not declared in the deployment", key)
	}
	return decl, nil
}

// registrarKinds is the ExecutionKinds() of every built Manager that depends on the schedule
// registrar component, in manager-key order, when component is that registrar. A Manager owns
// the kinds it runs; the registrar's constructor is handed them rather than a hook supplying a
// table the model already holds. skip names the Managers a generation leaves unbuilt (the
// scenario subject's refusals): their packages are not imported, so their kinds are not handed
// over.
func (r *resolved) registrarKinds(component string, skip map[string]Refusal) []string {
	reg := r.cfg.ScheduleRegistrarComponent
	if reg == "" || component != reg || r.m == nil {
		return nil
	}
	var out []string
	for _, key := range sortedKeys(r.m.Contracts) {
		c := r.m.Contracts[key]
		if _, unbuilt := skip[key]; !unbuilt && builtManagerDependingOn(c, reg) {
			out = append(out, r.aliasFor(c.GoPackage)+".ExecutionKinds()")
		}
	}
	return out
}

// builtManagerDependingOn reports a built Manager contract (the set resolveManagers walks) with
// a component dependency on key.
func builtManagerDependingOn(c *projectmodel.Contract, key string) bool {
	if c == nil || !strings.EqualFold(c.Layer, "manager") || c.Doc == nil || len(c.Deps) == 0 || c.GoPackage == "" {
		return false
	}
	return dependsOn(c, key)
}

// rootArgs spells constructor arguments for the composition root: the dialed satellites it
// holds (tc, ctx, pool), the generated Config's fields, and its typed Hooks.
type rootArgs struct{ r *resolved }

func (a rootArgs) infra(key string) (projectmodel.InfraDecl, error) { return a.r.infraDecl(key) }
func (rootArgs) temporal() []string                                 { return []string{"tc"} }
func (rootArgs) postgres() []string                                 { return []string{"ctx", "pool"} }

func (rootArgs) input(decl projectmodel.InfraDecl, in projectmodel.SubstrateInput) string {
	return "cfg." + infraFieldName(decl.Key, in.Name)
}

func (rootArgs) setting(s projectmodel.Setting) (string, error) {
	return "cfg." + settingFieldName(s.Name), nil
}

// hooked threads the shared Temporal client as an extra positional argument ahead of the hook
// call (the hook's own signature stays cfg-only: it reads its config, and tc is not a value it
// needs), so the hook composes as a plain multi-argument call.
func (a rootArgs) hooked(arm variantArm) ([]string, error) {
	var args []string
	for _, ik := range arm.infra {
		decl, err := a.r.infraDecl(ik)
		if err != nil {
			return nil, err
		}
		if decl.Substrate == projectmodel.SubstrateTemporal {
			args = append(args, a.temporal()...)
		}
	}
	return append(args, arm.hookCall), nil
}

func (a rootArgs) kinds(component string) []string { return a.r.registrarKinds(component, nil) }
