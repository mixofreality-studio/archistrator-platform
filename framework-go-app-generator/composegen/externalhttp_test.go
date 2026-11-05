package composegen_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// An ExternalHTTP binding's seeded args hook reads the endpoint URL the deployment declares from
// the generated Config (FIX-049). RUN-017's hand hook read APP_IDENTITY_PROVIDER_URL, an env var
// of its own invention that no launcher or gate could see.
func TestExternalHTTPHookSeedReadsTheDeclaredEndpoint(t *testing.T) {
	m, err := projectmodel.Load([]byte(`{"serviceContracts":{
	"identityAccess":{"component":"identityAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/identity","infra":["ExternalHTTP"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"IdentityAccess","layer":"resourceaccess","operations":[]}},
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list","$defs":{"T":{"type":"string"}},
	  "deps":[{"name":"client","goType":"client.Client","goImport":"go.temporal.io/sdk/client"},{"name":"identity","component":"identityAccess"}],
	  "interface":{"name":"ListManager","layer":"manager","operations":[]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.Deployment = m.EffectiveDeployment("server")
	cfg := composegen.Config{
		ContainerKey: "server", ModulePath: "example/server", PackageName: "main", EnvPrefix: "APP",
		VariantHookArgs: map[string][]composegen.HookArgType{
			"identityAccess/ExternalHTTP": {{GoType: "string"}, {GoType: "*http.Client", GoImport: "net/http"}},
		},
		VariantConstructorReturnsError: map[string]bool{"identityAccess/ExternalHTTP": true},
	}
	seed, err := composegen.HooksSeed(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "func (appHooks) IdentityAccessExternalHTTPArgs(cfg *Config) (string, *http.Client) {\n\treturn cfg.IdentityAccessURL, &http.Client{Timeout: 30 * time.Second}\n}"
	if !strings.Contains(string(seed), want) {
		t.Fatalf("the seeded hook must read the declared endpoint:\n%s", seed)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "hooks.go", seed, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var imports []string
	for _, i := range f.Imports {
		imports = append(imports, i.Path.Value)
	}
	if !strings.Contains(strings.Join(imports, " "), `"time"`) || !strings.Contains(strings.Join(imports, " "), `"net/http"`) {
		t.Errorf("the seed must import what it names, got %v", imports)
	}
}
