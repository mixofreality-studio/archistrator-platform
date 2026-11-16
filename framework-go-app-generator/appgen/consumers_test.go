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

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/webgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var fixtures = []string{"greenfield", "todomvc"}

// builtTree is a module tree where every contract with a goPackage holds one hand-written file.
func builtTree(t *testing.T, project []byte) fstest.MapFS {
	t.Helper()
	return builtTreeWithout(t, project, "")
}

// builtTreeWithout is builtTree minus the packages of one layer: a module part way through construction.
func builtTreeWithout(t *testing.T, project []byte, layer string) fstest.MapFS {
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
		if c.GoPackage != "" && !strings.EqualFold(c.Layer, layer) {
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

func TestConsumers_NamesAreTheNineOfD2(t *testing.T) {
	var got []string
	for _, c := range AllConsumers() {
		got = append(got, c.Name)
		if _, ok := ConsumerNamed(c.Name); !ok {
			t.Errorf("ConsumerNamed(%q) not found", c.Name)
		}
	}
	want := []string{"modelgen", "nullgen", "appgen temporal", "appgen config", "appgen compose", "appgen clients", "testgen", "webgen bootstrap", "webgen generate"}
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
func TestPlan_CoversPartialEmit(t *testing.T) {
	for _, fx := range fixtures {
		for _, c := range AllConsumers() {
			t.Run(fx+"/"+c.Name, func(t *testing.T) {
				in := fixtureInput(t, fx, "server", false)
				planned, err := c.Plan(in)
				if err != nil {
					t.Fatal(err)
				}
				partial := Input{Project: in.Project, ModulePath: in.ModulePath, ModuleDir: in.ModuleDir, Config: in.Config, Tree: builtTreeWithout(t, in.Project, "Client")}
				files, err := c.Emit(partial)
				if err != nil {
					t.Fatal(err)
				}
				have := map[string]plan.OutputRole{}
				for _, o := range planned {
					have[o.Path] = o.Role
				}
				for _, o := range plan.Outputs(files) {
					role, ok := have[o.Path]
					if !ok {
						t.Errorf("partial emission wrote %s, which Plan does not name", o.Path)
						continue
					}
					if plan.RoleName(role) != plan.RoleName(o.Role) {
						t.Errorf("%s: role %s in the emission, %s in Plan", o.Path, plan.RoleName(o.Role), plan.RoleName(role))
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
				t.Skipf("%s plans nothing for this fixture", c.Name)
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
		KindServiceContract:     {"modelgen", "nullgen", "appgen temporal", "appgen compose", "appgen clients", "testgen", "webgen generate"},
		KindTestPlan:            {"testgen"},
		KindUIDesign:            {"testgen"},
		KindAcceptancePlan:      {"testgen"},
		KindCoreUseCases:        {"testgen"},
		KindSystem:              {"appgen config", "appgen compose", "appgen clients", "testgen", "webgen bootstrap"},
		KindOperationalConcepts: {"appgen config", "appgen compose"},
	} {
		if got := names(kind); !slices.Equal(got, want) {
			t.Errorf("Consumers(%s) = %v, want %v", kind, got, want)
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
