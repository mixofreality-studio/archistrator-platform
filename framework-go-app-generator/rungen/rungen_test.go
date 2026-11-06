package rungen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/rungen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// freshProject is a fresh design shaped like RUN-017's: a Manager, a Postgres ResourceAccess, an
// ExternalHTTP ResourceAccess, a browser surface and an MCP client, with no authored deployment.
const freshProject = `{"serviceContracts":{
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list",
	  "deps":[{"name":"client","goType":"client.Client","goImport":"go.temporal.io/sdk/client"},{"name":"todos","component":"todoAccess"},{"name":"identity","component":"identityAccess"}],
	  "$defs":{"T":{"type":"string"}},"interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Ping","params":[],"error":false}]}},
	"todoAccess":{"component":"todoAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"TodoAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}},
	"identityAccess":{"component":"identityAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/identity","infra":["ExternalHTTP"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"IdentityAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}}},
	"slots":{"5":{"kind":5,"model":{"components":[
	  {"id":"todo-web-app","name":"TodoWebApp","kind":"client","uiSurface":true},
	  {"id":"assistant","name":"Assistant","kind":"client","uiSurface":false,"machineProtocol":"mcp"},
	  {"id":"list-manager","name":"ListManager","kind":"manager","contractKey":"listManager"}],
	  "relationships":[{"from":"todo-web-app","to":"list-manager"},{"from":"assistant","to":"list-manager"}]}}}}`

func freshModel(t *testing.T, project string) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.Load([]byte(project))
	if err != nil {
		t.Fatal(err)
	}
	m.Deployment = m.EffectiveDeployment("server")
	return m
}

var cfg = rungen.Config{ContainerKey: "server", EnvPrefix: "APP"}

// The run contract's coverage gate, both ways, over the generated texts (architect ruling,
// FIX-049): every input the generated Config requires under the local profile is provisioned or
// required from the caller by the launcher, and every variable the launcher names is one the
// Config reads. The ExternalHTTP endpoint is required from the caller, never faked.
func TestLauncherCoversTheLocalConfigBothWays(t *testing.T) {
	m := freshModel(t, freshProject)
	launcher := generate(t, m)
	cfgFiles, err := configgen.Generate(m, configgen.Config{ContainerKey: "server", EnvPrefix: "APP", PackageName: "main"})
	if err != nil {
		t.Fatal(err)
	}
	config := string(cfgFiles["config.gen.go"])

	read := map[string]bool{}
	for _, mm := range regexp.MustCompile(`getenv\w*\("(APP_[A-Z0-9_]+)"`).FindAllStringSubmatch(config, -1) {
		read[mm[1]] = true
	}
	if mm := regexp.MustCompile(`const devPrincipalEnv = "(APP_[A-Z0-9_]+)"`).FindStringSubmatch(config); mm != nil {
		read[mm[1]] = true
	}
	named := map[string]bool{}
	for _, mm := range regexp.MustCompile(`"(APP_[A-Z0-9_]+)"`).FindAllStringSubmatch(launcher, -1) {
		named[mm[1]] = true
		if !read[mm[1]] {
			t.Errorf("the launcher names %s, which the generated Config does not read", mm[1])
		}
	}
	local := regexp.MustCompile(`"local": \{([^}]*)\}`).FindStringSubmatch(config)
	if local == nil {
		t.Fatalf("config.gen.go has no local requirements:\n%s", config)
	}
	for _, mm := range regexp.MustCompile(`"(APP_[A-Z0-9_]+)"`).FindAllStringSubmatch(local[1], -1) {
		if !named[mm[1]] {
			t.Errorf("the Config requires %s under local, which the launcher neither provisions nor requires", mm[1])
		}
	}
	for _, want := range []string{
		`{Env: "APP_POSTGRES_URL", From: "postgres.url"}`,
		`{Env: "APP_TEMPORAL_HOSTPORT", From: "temporal.hostport"}`,
		`Required: []string{"APP_IDENTITY_ACCESS_URL"}`,
		`ListenEnv:       "APP_LISTEN_ADDR"`,
		`WebDistEnv:      "APP_WEB_DIST_DIR"`,
		`WebDir:          "webApp"`,
	} {
		if !strings.Contains(launcher, want) {
			t.Errorf("the launcher's plan lacks %q:\n%s", want, launcher)
		}
	}
}

// A design with no Postgres, no ExternalHTTP and no browser surface provisions only Temporal and
// builds no web project.
func TestLauncherProvisionsOnlyWhatTheModelDeclares(t *testing.T) {
	m := freshModel(t, `{"serviceContracts":{"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list",
	  "deps":[{"name":"client","goType":"client.Client","goImport":"go.temporal.io/sdk/client"}],
	  "$defs":{"T":{"type":"string"}},"interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Ping","params":[],"error":false}]}}}}`)
	p, err := rungen.Derive(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Temporal || p.Postgres || p.WebDir != "" || p.WebDistEnv != "" || len(p.Required) != 0 {
		t.Fatalf("plan = %+v", p)
	}
	if slices.Contains(p.Names(), "APP_POSTGRES_URL") {
		t.Errorf("a design without Postgres must not provision it: %v", p.Names())
	}
}

// Stopping the throwaway Postgres container also removes its anonymous data volume (FIX-063):
// the explicit `docker rm -f` takes over from `--rm`, and without -v every devstack boot orphaned
// the image's /var/lib/postgresql/data volume (570 of them, ~30GB, filled the bench host).
func TestLauncherRemovesThePostgresVolume(t *testing.T) {
	src := generate(t, freshModel(t, freshProject))
	if !strings.Contains(src, `exec.Command("docker", "rm", "-f", "-v", id)`) {
		t.Errorf("the launcher must remove the Postgres container with its volume (docker rm -f -v):\n%s", src)
	}
	if strings.Contains(src, `exec.Command("docker", "rm", "-f", id)`) {
		t.Errorf("the launcher still removes a container without its volume")
	}
}

// The generated launcher is a stdlib-only command that builds and vets on its own.
func TestLauncherBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a sandbox module")
	}
	dir := t.TempDir()
	src := generate(t, freshModel(t, freshProject))
	for name, content := range map[string]string{
		"go.mod":                                 "module example.com/fresh\n\ngo 1.25\n",
		filepath.Join(rungen.Dir, "main.gen.go"): src,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
}

func generate(t *testing.T, m *projectmodel.Model) string {
	t.Helper()
	got, err := rungen.Generate(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	src, ok := got[rungen.File]
	if !ok || !strings.HasPrefix(string(src), "// Code generated by rungen. DO NOT EDIT.") {
		t.Fatalf("no generated launcher: %v", got)
	}
	return string(src)
}
