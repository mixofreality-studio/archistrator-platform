package composegen

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// argsFixture is a resolved plan over one infra declaration of every substrate, the shared
// ground both composition roots spell constructor arguments over.
func argsFixture() *resolved {
	d := &projectmodel.Deployment{}
	for _, s := range projectmodel.Substrates() {
		d.Infrastructure = append(d.Infrastructure, projectmodel.InfraDecl{Key: "k-" + string(s), Substrate: s})
	}
	m := &projectmodel.Model{
		Deployment: d,
		Contracts: map[string]*projectmodel.Contract{
			"orderManager": {Layer: "Manager", GoPackage: "internal/manager/order", Doc: &projectmodel.Doc{}, Deps: []projectmodel.Dep{{Name: "bus", Component: "messageBus"}}},
			"auditManager": {Layer: "Manager", GoPackage: "internal/manager/audit", Doc: &projectmodel.Doc{}, Deps: []projectmodel.Dep{{Name: "bus", Component: "messageBus"}}},
			"idleManager":  {Layer: "Manager", GoPackage: "internal/manager/idle", Doc: &projectmodel.Doc{}, Deps: []projectmodel.Dep{{Name: "x", Component: "other"}}},
			"noDocManager": {Layer: "Manager", GoPackage: "internal/manager/nodoc", Deps: []projectmodel.Dep{{Name: "bus", Component: "messageBus"}}},
		},
	}
	r := newResolved(m, Config{ModulePath: "example.com/app", ScheduleRegistrarComponent: "messageBus"})
	r.assignAliases([]string{"internal/manager/audit", "internal/manager/idle", "internal/manager/order"})
	return r
}

// sources returns the two argSources over one plan: the composition root's and the generated
// test subject's.
func sources(r *resolved) (root, scenario argSource, g *scenarioGen) {
	g = &scenarioGen{r: r}
	return rootArgs{r: r}, scenarioArgs{r: r, g: g}, g
}

// Both composition roots spell an arm's constructor arguments through the one ctorArgs, so a
// substrate contributes the same number of values to either; only their spelling differs.
func TestCtorArgs_BothRootsSameShape(t *testing.T) {
	r := argsFixture()
	root, scenario, _ := sources(r)
	for _, s := range projectmodel.Substrates() {
		arm := variantArm{infra: []string{"k-" + string(s)}}
		rootGot, err := ctorArgs(arm, root)
		if err != nil {
			t.Fatalf("%s: root: %v", s, err)
		}
		scenGot, err := ctorArgs(arm, scenario)
		if err != nil {
			t.Fatalf("%s: scenario: %v", s, err)
		}
		if len(rootGot) != len(scenGot) {
			t.Errorf("%s: root spells %d arguments (%v), the scenario %d (%v)", s, len(rootGot), rootGot, len(scenGot), scenGot)
		}
		switch s {
		case projectmodel.SubstrateOtel:
			if len(rootGot) != 0 {
				t.Errorf("otel contributes no argument, got %v", rootGot)
			}
		case projectmodel.SubstrateTemporal:
			wantEq(t, s, rootGot, []string{"tc"})
			wantEq(t, s, scenGot, []string{"s.temporal()"})
		case projectmodel.SubstratePostgres:
			wantEq(t, s, rootGot, []string{"ctx", "pool"})
			wantEq(t, s, scenGot, []string{"s.t.Context()", "s.postgres()"})
		case projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateExternalHTTP, projectmodel.SubstrateGitRepo:
			if len(rootGot) != len(s.Inputs()) {
				t.Errorf("%s: want one argument per catalog input (%d), got %v", s, len(s.Inputs()), rootGot)
				continue
			}
			for i, in := range s.Inputs() {
				wantEq(t, s, rootGot[i:i+1], []string{"cfg." + projectmodel.InfraFieldName("k-"+string(s), in.Name)})
				want := `s.h.Input(s.t, "k-` + string(s) + `", "` + string(s) + `", "` + in.Name + `")`
				wantEq(t, s, scenGot[i:i+1], []string{want})
			}
		}
	}
}

func wantEq(t *testing.T, s projectmodel.Substrate, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: got %v, want %v", s, got, want)
	}
}

// An infra key the deployment does not declare is an error from either root, never a silently
// empty argument list.
func TestCtorArgs_UnknownInfraIsAnError(t *testing.T) {
	root, scenario, _ := sources(argsFixture())
	for name, src := range map[string]argSource{"root": root, "scenario": scenario} {
		if _, err := ctorArgs(variantArm{infra: []string{"ghost"}}, src); err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Errorf("%s: want an error naming the undeclared infra key, got %v", name, err)
		}
	}
}

// Binding settings follow the infra values: cfg.<Field> in production, the declared default in
// a scenario.
func TestCtorArgs_SettingsFollowInfra(t *testing.T) {
	root, scenario, g := sources(argsFixture())
	arm := variantArm{
		infra:    []string{"k-" + string(projectmodel.SubstrateTemporal)},
		settings: []projectmodel.Setting{{Name: "shutdownTimeout", Type: "duration", Default: "20s"}},
	}
	got, err := ctorArgs(arm, root)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "root", got, []string{"tc", "cfg.ShutdownTimeout"})
	got, err = ctorArgs(arm, scenario)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "scenario", got, []string{"s.temporal()", "time.Duration(20000000000)"})
	if !g.usesTemporal || !g.usesTime {
		t.Errorf("the scenario must record the temporal client and time it spelled (temporal=%v time=%v)", g.usesTemporal, g.usesTime)
	}
}

// The registrar component's constructor takes one ExecutionKinds() per Manager that depends
// on it, in manager-key order, from both roots.
func TestCtorArgs_RegistrarKindsFromManagers(t *testing.T) {
	root, scenario, _ := sources(argsFixture())
	arm := variantArm{component: "messageBus", infra: []string{"k-" + string(projectmodel.SubstrateTemporal)}}
	for name, src := range map[string]argSource{"root": root, "scenario": scenario} {
		got, err := ctorArgs(arm, src)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[1] != "audit.ExecutionKinds()" || got[2] != "order.ExecutionKinds()" {
			t.Errorf("%s: want [<temporal>, audit.ExecutionKinds(), order.ExecutionKinds()], got %v", name, got)
		}
	}
	other := variantArm{component: "somethingElse"}
	if got, _ := ctorArgs(other, root); len(got) != 0 {
		t.Errorf("a component that is not the registrar takes no kinds, got %v", got)
	}
}

// A hook-supplied arm: the root threads the shared client then calls its hook; the scenario
// has no hooks to ask.
func TestCtorArgs_HookedArm(t *testing.T) {
	root, scenario, _ := sources(argsFixture())
	arm := variantArm{component: "orderStateAccess", variant: "GitLocal", hooked: true, hookCall: "hooks.OrderStateAccessGitLocalArgs(cfg)", infra: []string{"k-" + string(projectmodel.SubstrateTemporal)}}
	got, err := ctorArgs(arm, root)
	if err != nil {
		t.Fatal(err)
	}
	wantEq(t, "root", got, []string{"tc", "hooks.OrderStateAccessGitLocalArgs(cfg)"})
	if _, err := ctorArgs(arm, scenario); err == nil || !strings.Contains(err.Error(), "orderStateAccess") {
		t.Errorf("the scenario must refuse a hooked arm, naming it: %v", err)
	}
}

// localArm is the binding's local-profile arm: the run contract. A required binding with none
// is the composition root's own "no variant" error; an optional one is simply not built; a
// hooked local arm is refused.
func TestLocalArm(t *testing.T) {
	arms := func(profiles ...string) []variantArm {
		var out []variantArm
		for _, p := range profiles {
			out = append(out, variantArm{profile: p, variant: "v-" + p})
		}
		return out
	}
	t.Run("picks the local arm", func(t *testing.T) {
		arm, ok, err := localArm(raBinding{key: "a", arms: arms("cloud", "local")})
		if err != nil || !ok || arm.profile != projectmodel.LocalProfile {
			t.Fatalf("got %+v ok=%v err=%v", arm, ok, err)
		}
	})
	t.Run("a stub binding is its stub arm", func(t *testing.T) {
		arm, ok, err := localArm(raBinding{key: "a", stub: true, arms: []variantArm{{variant: "stub"}}})
		if err != nil || !ok || arm.variant != "stub" {
			t.Fatalf("got %+v ok=%v err=%v", arm, ok, err)
		}
	})
	t.Run("required with no local arm", func(t *testing.T) {
		_, _, err := localArm(raBinding{key: "a", presence: "required", layer: "ResourceAccess", arms: arms("cloud")})
		if err == nil || !strings.Contains(err.Error(), "a: no ResourceAccess variant for the active profile") {
			t.Fatalf("want the root's no-variant text, got %v", err)
		}
	})
	for _, presence := range []string{"optional", "optional-dormant"} {
		t.Run(presence+" with no local arm", func(t *testing.T) {
			_, ok, err := localArm(raBinding{key: "a", presence: presence, arms: arms("cloud")})
			if err != nil || ok {
				t.Fatalf("want (not built, no error), got ok=%v err=%v", ok, err)
			}
		})
	}
	t.Run("hooked local arm", func(t *testing.T) {
		rb := raBinding{key: "a", presence: "required", arms: []variantArm{{profile: "local", variant: "GitLocal", hooked: true}}}
		_, _, err := localArm(rb)
		if err == nil || !strings.Contains(err.Error(), "model-derived") || !strings.Contains(err.Error(), "a") {
			t.Fatalf("want the run-contract error, got %v", err)
		}
	})
}
