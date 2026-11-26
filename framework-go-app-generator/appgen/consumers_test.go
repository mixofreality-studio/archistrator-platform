package appgen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/webgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var fixtures = []string{"greenfield", "todomvc"}

// builtTree is a module tree where every contract with a goPackage holds one hand-written file.
func builtTree(t *testing.T, project []byte) fstest.MapFS {
	t.Helper()
	return builtTreeOf(t, project)
}

// builtTreeOf holds one hand-written file for every contract with a goPackage in the named layers
// (all layers when none is named): a module part way through construction.
func builtTreeOf(t *testing.T, project []byte, layers ...string) fstest.MapFS {
	t.Helper()
	var doc struct {
		ServiceContracts map[string]struct {
			GoPackage string `json:"goPackage"`
			Layer     string `json:"layer"`
		} `json:"serviceContracts"`
	}
	if err := json.Unmarshal(project, &doc); err != nil {
		t.Fatal(err)
	}
	tree := fstest.MapFS{}
	for _, c := range doc.ServiceContracts {
		wanted := len(layers) == 0 || slices.ContainsFunc(layers, func(l string) bool { return strings.EqualFold(c.Layer, l) })
		if c.GoPackage != "" && wanted {
			tree[c.GoPackage+"/impl.go"] = &fstest.MapFile{Data: []byte("package x\n")}
		}
	}
	return tree
}

func fixtureInput(t *testing.T, name, moduleDir string, built bool) Input {
	t.Helper()
	raw := testfixtures.Fixture(t, name)
	in := Input{Project: raw, ModulePath: "example.com/app", ModuleDir: moduleDir, Config: DefaultConfig()}
	if built {
		in.Tree = builtTree(t, raw)
	}
	return in
}

func TestConsumers_EveryArtifactKindHasAtLeastOne(t *testing.T) {
	for _, k := range ArtifactKinds() {
		if len(Consumers(k)) == 0 {
			t.Errorf("artifact kind %q has no consumer", k)
		}
	}
	if Consumers("nope") != nil {
		t.Error("an unknown kind must have no consumers")
	}
}

func TestConsumers_NamesAreTheTenOfD2(t *testing.T) {
	var got []string
	for _, c := range AllConsumers() {
		got = append(got, c.Name)
		if _, ok := ConsumerNamed(c.Name); !ok {
			t.Errorf("ConsumerNamed(%q) not found", c.Name)
		}
	}
	want := []string{"modelgen", "nullgen", "appgen temporal", "appgen config", "appgen compose", "appgen scenariocompose", "appgen clients", "testgen", "webgen bootstrap", "webgen generate"}
	if !slices.Equal(got, want) {
		t.Fatalf("consumers = %v, want %v", got, want)
	}
	if _, ok := ConsumerNamed("nope"); ok {
		t.Error("an unknown consumer name must not resolve")
	}
}

// Emit over a fully built tree writes exactly the paths Plan names, each with the role Plan gives it.
func TestPlan_MatchesEmit(t *testing.T) {
	for _, fx := range fixtures {
		for _, c := range AllConsumers() {
			t.Run(fx+"/"+c.Name, func(t *testing.T) {
				in := fixtureInput(t, fx, "server", true)
				planned, err := c.Plan(in)
				if err != nil {
					t.Fatal(err)
				}
				files, err := c.Emit(in)
				if err != nil {
					t.Fatal(err)
				}
				if got := plan.Outputs(files); !reflect.DeepEqual(got, planned) {
					t.Fatalf("Emit wrote %v, Plan says %v", got, planned)
				}
			})
		}
	}
}

// Plan treats every contract as built (Tree == nil); a module part way through construction only
// ever emits a subset of it.
//
// The trees really are partial: an empty one (nothing built) and one holding only the
// ResourceAccess and Engine packages. todomvc's mcpApp fronts a Manager that is not built yet: compose
// leaves it unbuilt with that Manager rather than refusing, so it emits like every other consumer.
// The non-vacuity guard fails unless, per fixture, some consumer emits strictly fewer files than Plan.
func TestPlan_CoversPartialEmit(t *testing.T) {
	strictlyFewer := map[string][]string{}
	for _, fx := range fixtures {
		for _, c := range AllConsumers() {
			t.Run(fx+"/"+c.Name, func(t *testing.T) {
				in := fixtureInput(t, fx, "server", false)
				planned, err := c.Plan(in)
				if err != nil {
					t.Fatal(err)
				}
				have := map[string]plan.OutputRole{}
				for _, o := range planned {
					have[o.Path] = o.Role
				}
				trees := map[string]fstest.MapFS{
					"empty":                  {},
					"resourceaccess+engines": builtTreeOf(t, in.Project, "ResourceAccess", "Engine"),
				}
				for name, tree := range trees {
					partial := in
					partial.Tree = tree
					files, err := c.Emit(partial)
					if err != nil {
						t.Errorf("%s tree: %v", name, err)
						continue
					}
					if len(files) < len(planned) {
						strictlyFewer[fx] = append(strictlyFewer[fx], c.Name+" ("+name+")")
					}
					for _, o := range plan.Outputs(files) {
						role, ok := have[o.Path]
						if !ok {
							t.Errorf("%s tree: the partial emission wrote %s, which Plan does not name", name, o.Path)
							continue
						}
						if role != o.Role {
							t.Errorf("%s tree: %s has role %T in the emission, %T in Plan", name, o.Path, o.Role, role)
						}
					}
				}
			})
		}
	}
	for _, fx := range fixtures {
		if len(strictlyFewer[fx]) == 0 {
			t.Errorf("%s: no consumer emits fewer files over a partial tree than Plan names: the subset proof is vacuous", fx)
		}
	}
}

// Plan ignores the caller's Tree: it is the emission of a fully built module, whatever the module
// holds now. Edit scope computed from Plan part way through construction depends on it.
func TestPlan_IgnoresTheTree(t *testing.T) {
	for _, fx := range fixtures {
		for _, c := range AllConsumers() {
			t.Run(fx+"/"+c.Name, func(t *testing.T) {
				in := fixtureInput(t, fx, "server", false)
				base, err := c.Plan(in)
				if err != nil {
					t.Fatal(err)
				}
				if len(base) == 0 && fx == "todomvc" { // greenfield has no web or MCP surface for three consumers
					t.Fatalf("%s plans nothing for %s: the proof is vacuous", c.Name, fx)
				}
				built := in
				built.Tree = builtTree(t, in.Project)
				files, err := c.Emit(built)
				if err != nil {
					t.Fatal(err)
				}
				if got := plan.Outputs(files); !reflect.DeepEqual(got, base) {
					t.Errorf("Plan = %v, the emission of the built tree = %v", base, got)
				}
				for name, tree := range map[string]fstest.MapFS{
					"empty":                  {},
					"resourceaccess+engines": builtTreeOf(t, in.Project, "ResourceAccess", "Engine"),
					"built":                  builtTree(t, in.Project),
				} {
					given := in
					given.Tree = tree
					got, err := c.Plan(given)
					if err != nil {
						t.Errorf("%s tree: Plan: %v", name, err)
						continue
					}
					if !reflect.DeepEqual(got, base) {
						t.Errorf("%s tree: Plan = %v, want the tree-less %v", name, got, base)
					}
				}
			})
		}
	}
}

// Every path is repo-relative: the module's under ModuleDir, the web project's under webApp/, the
// Playwright output under uitests/.
func TestPlan_IsRepoRelative(t *testing.T) {
	for _, c := range AllConsumers() {
		t.Run(c.Name, func(t *testing.T) {
			outs, err := c.Plan(fixtureInput(t, "todomvc", "server", false))
			if err != nil {
				t.Fatal(err)
			}
			if len(outs) == 0 {
				t.Fatalf("%s plans nothing for the todomvc fixture, which plans at least one file for every consumer", c.Name)
			}
			for _, o := range outs {
				switch {
				case strings.HasPrefix(o.Path, "webApp/"), strings.HasPrefix(o.Path, "uitests/"):
					if !strings.HasPrefix(c.Name, "webgen") && c.Name != "testgen" {
						t.Errorf("%s: %s is outside the module", c.Name, o.Path)
					}
				case strings.HasPrefix(o.Path, "server/"):
				default:
					t.Errorf("%s: %s is not repo-relative (want server/, webApp/ or uitests/)", c.Name, o.Path)
				}
				if strings.Contains(o.Path, "..") || strings.HasPrefix(o.Path, "/") {
					t.Errorf("%s: %s is not a clean repo-relative path", c.Name, o.Path)
				}
			}
		})
	}
}

func rolesOf(t *testing.T, name string, in Input) map[string]plan.OutputRole {
	t.Helper()
	c, ok := ConsumerNamed(name)
	if !ok {
		t.Fatalf("no consumer %q", name)
	}
	outs, err := c.Plan(in)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]plan.OutputRole{}
	for _, o := range outs {
		m[o.Path] = o.Role
	}
	return m
}

func TestPlan_HooksIsASeam_McpAppSeatIsGenerated(t *testing.T) {
	in := fixtureInput(t, "todomvc", "server", false)
	compose := rolesOf(t, "appgen compose", in)
	if r, ok := compose["server/cmd/server/hooks.go"]; !ok || plan.RoleName(r) != "seam" {
		t.Errorf("appgen compose: hooks.go = %v (planned %v), want a Seam", r, ok)
	}
	if r, ok := compose["server/cmd/server/main.gen.go"]; !ok || plan.RoleName(r) != "generated" {
		t.Errorf("appgen compose: main.gen.go = %v (planned %v), want Generated", r, ok)
	}
	boot := rolesOf(t, "webgen bootstrap", in)
	if r, ok := boot["webApp/src/todo-mcp-app/main.tsx"]; !ok || plan.RoleName(r) != "generated" {
		t.Errorf("webgen bootstrap: the mcpApp host-bridge seat = %v (planned %v), want Generated; plan: %v", r, ok, boot)
	}
	if r, ok := boot["webApp/src/todo-mcp-app/routes/App.tsx"]; !ok || plan.RoleName(r) != "seam" {
		t.Errorf("webgen bootstrap: the mcpApp's App.tsx = %v (planned %v), want a Seam", r, ok)
	}
	if r, ok := boot["webApp/webgen.json"]; !ok || plan.RoleName(r) != "seam" {
		t.Errorf("webgen bootstrap: webgen.json = %v (planned %v), want a Seam", r, ok)
	}
	null := rolesOf(t, "nullgen", in)
	if r, ok := null["server/cmd/server/hooks.go"]; !ok || plan.RoleName(r) != "seam" {
		t.Errorf("nullgen: hooks.go = %v (planned %v), want a Seam", r, ok)
	}
}

// A web client's bootstrap is all seats the app owns after.
func TestPlan_WebBootstrapIsAllSeams(t *testing.T) {
	boot := rolesOf(t, "webgen bootstrap", fixtureInput(t, "todomvc", "server", false))
	for p, r := range boot {
		if strings.HasPrefix(p, "webApp/src/todo-web-app/") && plan.RoleName(r) != "seam" {
			t.Errorf("%s is %s, want a Seam", p, plan.RoleName(r))
		}
	}
	if _, ok := boot["webApp/todo-web-app.html"]; !ok {
		t.Errorf("the web surface's page is not planned: %v", boot)
	}
}

// `webgen generate` binds the transport the server emits, read from the clients' emission in
// memory: the files are the ones webgen renders from the same emission written to disk.
func TestWebGenerate_InMemoryEqualsFromDisk(t *testing.T) {
	in := fixtureInput(t, "todomvc", "", true)
	generate, _ := ConsumerNamed("webgen generate")
	got, err := generate.Emit(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("webgen generate emits nothing for the todomvc fixture: the proof is vacuous")
	}
	clients, _ := ConsumerNamed("appgen clients")
	emitted, err := clients.Emit(in)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	for _, f := range emitted {
		writeTemp(t, filepath.Join(repo, filepath.FromSlash(f.Path)), f.Content)
	}
	cfg, err := in.webConfig(projectmodel.SystemComponent{ID: "todo-web-app", Surface: projectmodel.SurfaceWeb})
	if err != nil {
		t.Fatal(err)
	}
	disk, err := webgen.LoadInputs(filepath.Join(repo, "webApp"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := webgen.Generate(disk)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("from memory: %d files, from disk: %d", len(got), len(want))
	}
	for i, f := range want {
		if got[i].Path != "webApp/"+f.Path || !bytes.Equal(got[i].Content, f.Content) || plan.RoleName(got[i].Role) != "generated" {
			t.Errorf("%s differs between the in-memory and the on-disk read", f.Path)
		}
	}
}

// A design recorded before its first Manager contract still generates its browser surface: the
// clients' emission is an OpenAPI document with no paths and no tool table, and webgen binds
// zero operations rather than refusing (ISS-027-4: every such activity failed admission:tier2).
func TestWebGenerate_BeforeAnyManagerContract(t *testing.T) {
	raw := testfixtures.Fixture(t, "todomvc")
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["serviceContracts"] = json.RawMessage(`{}`)
	bare, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Project: bare, ModulePath: "example.com/app", Config: DefaultConfig()}
	clients, _ := ConsumerNamed("appgen clients")
	emitted, err := clients.Emit(in)
	if err != nil {
		t.Fatal(err)
	}
	var oas bool
	for _, f := range emitted {
		oas = oas || f.Path == "api/openapi.yaml"
	}
	if !oas {
		t.Fatalf("appgen clients must emit api/openapi.yaml even with no exposed Manager: %v", emitted)
	}
	generate, _ := ConsumerNamed("webgen generate")
	got, err := generate.Emit(in)
	if err != nil {
		t.Fatalf("webgen generate before any Manager contract: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("webgen generate emits nothing: the surface has no transport")
	}
}

func writeTemp(t *testing.T, p string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A webgen.json in the web project's tree configures the transport (here the preview shell's
// generated files join it); with the module under server/ the default config follows the module.
func TestWebGenerate_ConfigComesFromTheWebTree(t *testing.T) {
	in := fixtureInput(t, "todomvc", "server", true)
	generate, _ := ConsumerNamed("webgen generate")
	plain, err := generate.Plan(in)
	if err != nil {
		t.Fatal(err)
	}
	in.WebTree = fstest.MapFS{"webgen.json": &fstest.MapFile{Data: []byte(`{"app":"todomvc","surface":"todo-web-app","oas":"../server/api/openapi.yaml","mcpTools":"../server/internal/client","mcpOptional":true,"preview":true,"fixturesEnv":"TODOMVC_PREVIEW_FIXTURES"}`)}}
	preview, err := generate.Plan(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) <= len(plain) {
		t.Fatalf("preview: true must add the fixture schema and its test: %v vs %v", preview, plain)
	}
}

// The consumer table of D2: which artifact kind reaches which generators.
func TestConsumers_KindTable(t *testing.T) {
	names := func(k ArtifactKind) []string {
		var out []string
		for _, c := range Consumers(k) {
			out = append(out, c.Name)
		}
		return out
	}
	for kind, want := range map[ArtifactKind][]string{
		KindServiceContract:     {"modelgen", "nullgen", "appgen temporal", "appgen compose", "appgen scenariocompose", "appgen clients", "testgen", "webgen generate"},
		KindTestPlan:            {"testgen"},
		KindUIDesign:            {"testgen"},
		KindAcceptancePlan:      {"testgen"},
		KindCoreUseCases:        {"testgen"},
		KindSystem:              {"appgen config", "appgen compose", "appgen scenariocompose", "appgen clients", "testgen", "webgen bootstrap"},
		KindOperationalConcepts: {"appgen config", "appgen compose", "appgen scenariocompose"},
	} {
		if got := names(kind); !slices.Equal(got, want) {
			t.Errorf("Consumers(%s) = %v, want %v", kind, got, want)
		}
	}
}

// The generated test subject belongs to its own consumer, not to the composition root: an Engine
// or ResourceAccess component's scenario tests need it without cmd/server being generated.
func TestScenarioCompose_IsItsOwnConsumer(t *testing.T) {
	for _, fx := range fixtures {
		in := fixtureInput(t, fx, "server", false)
		subject := "server/" + composegen.ScenarioComposeFile
		if _, ok := rolesOf(t, "appgen compose", in)[subject]; ok {
			t.Errorf("%s: appgen compose still plans %s", fx, subject)
		}
		got := rolesOf(t, "appgen scenariocompose", in)
		if len(got) != 1 {
			t.Errorf("%s: appgen scenariocompose plans %v, want exactly %s", fx, got, subject)
		}
		if r, ok := got[subject]; !ok || plan.RoleName(r) != "generated" {
			t.Errorf("%s: %s = %v (planned %v), want Generated", fx, subject, r, ok)
		}
	}
}

// Over a tree holding only an Engine package's code, the test subject names no package that is
// not built, so it compiles at whatever point construction has reached.
func TestScenarioCompose_EngineOnlyTreeNamesNoUnbuiltPackage(t *testing.T) {
	for _, fx := range fixtures {
		in := fixtureInput(t, fx, "server", false)
		in.Tree = builtTreeOf(t, in.Project, "Engine")
		var doc struct {
			ServiceContracts map[string]struct {
				GoPackage string `json:"goPackage"`
				Layer     string `json:"layer"`
			} `json:"serviceContracts"`
		}
		if err := json.Unmarshal(in.Project, &doc); err != nil {
			t.Fatal(err)
		}
		c, _ := ConsumerNamed("appgen scenariocompose")
		files, err := c.Emit(in)
		if err != nil {
			t.Fatalf("%s: %v", fx, err)
		}
		if len(files) != 1 {
			t.Fatalf("%s: emitted %d files, want 1", fx, len(files))
		}
		src := string(files[0].Content)
		engines := 0
		for key, sc := range doc.ServiceContracts {
			if sc.GoPackage == "" {
				continue
			}
			named := strings.Contains(src, `"example.com/app/`+sc.GoPackage+`"`)
			isEngine := strings.EqualFold(sc.Layer, "engine")
			if isEngine {
				engines++
			}
			if named && !isEngine {
				t.Errorf("%s: the test subject names %s (%s), which is not built", fx, sc.GoPackage, key)
			}
		}
		if engines == 0 {
			t.Logf("%s: no Engine contract; nothing to prove", fx)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Container != "server" || c.EnvPrefix != "APP" {
		t.Fatalf("DefaultConfig = %+v", c)
	}
}

func TestParseConfig_RefusesUnknownFields(t *testing.T) {
	if _, err := ParseConfig([]byte(`{"nope": 1}`)); err == nil {
		t.Fatal("an unknown appgen.json field must be refused")
	}
	c, err := ParseConfig([]byte(`{"envPrefix": "TODO"}`))
	if err != nil || c.EnvPrefix != "TODO" || c.Container != "server" {
		t.Fatalf("ParseConfig = %+v, %v", c, err)
	}
}
