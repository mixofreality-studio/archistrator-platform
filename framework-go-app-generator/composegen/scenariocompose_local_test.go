package composegen_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// TestScenarioCompose_ConfigSubstrateIsReadFromTheHost: a local arm that consumes an infrastructure
// input the model declares (a git repository, a GitHub App) is spelled from the scenario host's
// Input - the one infra-to-substrate mapping - never from config the test does not have and never
// as an empty value.
func TestScenarioCompose_ConfigSubstrateIsReadFromTheHost(t *testing.T) {
	m := loadScenarioFixture(t)
	m.Deployment.Infrastructure = append(m.Deployment.Infrastructure,
		projectmodel.InfraDecl{Key: "order-repo", Substrate: projectmodel.SubstrateGitRepo, Profiles: []string{"local"}, Presence: "required"})
	for i, b := range m.Deployment.Bindings {
		if b.Component == "orderStateAccess" {
			m.Deployment.Bindings[i].PerProfile = map[string]projectmodel.BindingVariant{
				"local": {Variant: "GitLocal", Infra: []string{"order-repo", "github-app"}},
			}
		}
	}
	src, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		`orderstate.NewGitLocalAccess(s.h.Input(s.t, "order-repo", "git-repo", "URL"), s.h.Input(s.t, "github-app", "github-app", "APP_ID")`,
		`s.h.Input(s.t, "github-app", "github-app", "PRIVATE_KEY_PEM")`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "cfg.") {
		t.Error("a scenario subject has no config to read")
	}
}

// TestScenarioCompose_LocalArmOnly: the subject is the local-profile composition - the run contract
// cmd/devstack boots - so the binding's local arm is chosen even where another profile's arm
// consumes more infrastructure.
func TestScenarioCompose_LocalArmOnly(t *testing.T) {
	m := loadScenarioFixture(t)
	src, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "orderstate.NewMemoryAccess()") {
		t.Errorf("want the local (memory) arm:\n%s", s)
	}
	for _, banned := range []string{"NewPostgresOrderStateAccess", "s.postgres()", "postgresinfra"} {
		if strings.Contains(s, banned) {
			t.Errorf("a cloud arm leaked into the local composition: %q", banned)
		}
	}
}

// TestScenarioCompose_OptionalDormantNoLocalArmIsNil: an optional binding with no local arm is not
// built, as in main.gen.go: its Manager receives nil and its Worker stays unregistered.
func TestScenarioCompose_OptionalDormantNoLocalArmIsNil(t *testing.T) {
	m := loadScenarioFixture(t)
	for i, b := range m.Deployment.Bindings {
		if b.Component == "orderStateAccess" {
			m.Deployment.Bindings[i].PerProfile = map[string]projectmodel.BindingVariant{"cloud": b.PerProfile["cloud"]}
		}
	}
	src, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	s := string(src)
	if strings.Contains(s, "buildOrderStateAccess") {
		t.Errorf("an optional binding with no local arm must not be built:\n%s", s)
	}
	if !strings.Contains(s, "order.NewOrderManager(s.temporal(), nil, buildPricingEngine(s)") {
		t.Errorf("the Manager must receive nil for the unbuilt dependency:\n%s", s)
	}
}

// TestScenarioCompose_RequiredNoLocalArmIsError: a required binding with no local arm fails
// generation with the composition root's own text.
func TestScenarioCompose_RequiredNoLocalArmIsError(t *testing.T) {
	m := loadScenarioFixture(t)
	for i, b := range m.Deployment.Bindings {
		if b.Component == "orderStateAccess" {
			m.Deployment.Bindings[i].Presence = "required"
			m.Deployment.Bindings[i].PerProfile = map[string]projectmodel.BindingVariant{"cloud": b.PerProfile["cloud"]}
		}
	}
	_, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err == nil || !strings.Contains(err.Error(), "orderStateAccess: no ResourceAccess variant for the active profile") {
		t.Fatalf("want the composition root's no-variant error, got %v", err)
	}
}

// TestScenarioCompose_LocalHookedArmIsError: the local profile is the run contract, so a local
// arm whose arguments only the app's hand hooks can supply is a model error, not a thing a
// generated subject can paper over.
func TestScenarioCompose_LocalHookedArmIsError(t *testing.T) {
	m := loadScenarioFixture(t)
	cfg := scenarioFixtureCfg
	cfg.VariantHookArgs = map[string][]composegen.HookArgType{"orderStateAccess/memory": {{GoType: "string"}}}
	_, _, err := composegen.EmitScenarioCompose(m, cfg)
	if err == nil {
		t.Fatal("want a generation error for a hooked local arm")
	}
	for _, want := range []string{"orderStateAccess", "memory", "VariantHookArgs", "the local profile is the run contract and must be model-derived; declare its inputs as infrastructure or binding settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q, got: %v", want, err)
		}
	}
}

// settingLinked is the scenariocompose fixture whose orderManager takes its shutdown timeout
// through a dependency named differently from the setting, linked by Dep.Setting.
func settingLinked(t *testing.T) *projectmodel.Model {
	t.Helper()
	m := loadScenarioFixture(t)
	deps := m.Contracts["orderManager"].Deps
	for i := range deps {
		if deps[i].Name == "shutdownTimeout" {
			deps[i].Name, deps[i].Setting = "closeAfter", "shutdownTimeout"
		}
	}
	return m
}

// TestCompose_DepSettingBothRoots: a Manager dependency linked to a deployment setting reads
// cfg.<Field> in the composition root and the declared default in a scenario, and asks no
// resolver hook in either.
func TestCompose_DepSettingBothRoots(t *testing.T) {
	m := settingLinked(t)
	cfg := composegen.Config{ContainerKey: "order-app", ModulePath: scenarioFixtureCfg.ModulePath, PackageName: "main", EnvPrefix: "ORDER"}
	got, err := composegen.Generate(m, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	root := string(got["main.gen.go"])
	if !strings.Contains(root, "cfg.ShutdownTimeout") || strings.Contains(root, "CloseAfter") {
		t.Errorf("the root must read the linked setting and emit no resolver hook for it:\n%s", root)
	}
	src, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	if !strings.Contains(string(src), "time.Duration(20000000000)") {
		t.Errorf("the scenario must spell the setting's default:\n%s", src)
	}

	m.Contracts["orderManager"].Deps[4].Setting = "noSuchSetting"
	if _, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg); err == nil || !strings.Contains(err.Error(), "noSuchSetting") {
		t.Errorf("a dependency linked to an undeclared setting must fail the scenario, naming it: %v", err)
	}
}

// TestScenarioCompose_LocalPostgresArm: a local arm on Postgres is built over the scenario host's
// shared pool, which the file then declares.
func TestScenarioCompose_LocalPostgresArm(t *testing.T) {
	m := loadScenarioFixture(t)
	for i, b := range m.Deployment.Bindings {
		if b.Component == "orderStateAccess" {
			m.Deployment.Bindings[i].PerProfile = map[string]projectmodel.BindingVariant{"local": b.PerProfile["cloud"]}
		}
	}
	src, _, err := composegen.EmitScenarioCompose(m, scenarioFixtureCfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	for _, want := range []string{"orderstate.NewPostgresAccess(s.t.Context(), s.postgres())", "func (s *subject) postgres() *pgxpool.Pool"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

// TestCompose_RegistrarKindsFromManagers: the schedule registrar's constructor takes the
// ExecutionKinds of every Manager that depends on it, in both composition roots.
func TestCompose_RegistrarKindsFromManagers(t *testing.T) {
	m, err := projectmodel.Load([]byte(strings.ReplaceAll(messageBusHookFixture, "prod", "local")))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	cfg := messageBusHookCfg()
	cfg.VariantHookArgs = nil // the kinds come from the model, not a hook
	got, err := composegen.Generate(m, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if root := string(got["main.gen.go"]); !strings.Contains(root, "messagebus.NewTemporalMessageBus(tc, order.ExecutionKinds())") {
		t.Errorf("the root must hand the registrar its Managers' kinds:\n%s", root)
	}
	src, _, err := composegen.EmitScenarioCompose(m, cfg)
	if err != nil {
		t.Fatalf("EmitScenarioCompose: %v", err)
	}
	if !strings.Contains(string(src), "messagebus.NewTemporalMessageBus(s.temporal(), order.ExecutionKinds())") {
		t.Errorf("the scenario must hand the registrar its Managers' kinds:\n%s", src)
	}
}
