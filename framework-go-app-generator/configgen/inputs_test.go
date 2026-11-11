package configgen_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// freshModel is a fresh design's model with no authored deployment: a Manager, a Postgres
// ResourceAccess and an ExternalHTTP one, so its effective deployment is the derived default.
func freshModel(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.Load([]byte(`{"serviceContracts":{
	"listManager":{"component":"listManager","layer":"Manager","goPackage":"internal/manager/list","$defs":{"T":{"type":"string"}},
	  "interface":{"name":"ListManager","layer":"manager","operations":[{"name":"Ping","params":[],"error":false}]}},
	"todoAccess":{"component":"todoAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"TodoAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}},
	"identityAccess":{"component":"identityAccess","layer":"ResourceAccess","goPackage":"internal/resourceaccess/identity","infra":["ExternalHTTP"],"$defs":{"T":{"type":"string"}},
	  "interface":{"name":"IdentityAccess","layer":"resourceaccess","operations":[{"name":"Ping","params":[],"error":false}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	m.Deployment = m.EffectiveDeployment("server")
	return m
}

var freshCfg = configgen.Config{ContainerKey: "server", EnvPrefix: "APP", PackageName: "main"}

// Inputs names exactly the variables config.gen.go reads, so a launcher or a gate never derives
// an env name of its own (FIX-049). The ExternalHTTP endpoint is one of them, required locally.
func TestInputsAreTheVariablesTheConfigReads(t *testing.T) {
	m := freshModel(t)
	inputs, err := configgen.Inputs(m, freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	files, err := configgen.Generate(m, freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["config.gen.go"])
	read := map[string]bool{}
	for _, mm := range regexp.MustCompile(`getenv\w*\("(APP_[A-Z0-9_]+)"`).FindAllStringSubmatch(src, -1) {
		read[mm[1]] = true
	}
	read["APP_DEV_PRINCIPAL"] = strings.Contains(src, `const devPrincipalEnv = "APP_DEV_PRINCIPAL"`)
	read["APP_DEV_PRINCIPALS"] = strings.Contains(src, `const devPrincipalsEnv = "APP_DEV_PRINCIPALS"`)
	read["APP_SCENARIOHOST_URL"] = strings.Contains(src, `const scenariohostURLEnv = "APP_SCENARIOHOST_URL"`)
	var names []string
	for _, in := range inputs {
		names = append(names, in.Env)
		if !read[in.Env] {
			t.Errorf("Inputs names %s, which config.gen.go does not read", in.Env)
		}
	}
	for env := range read {
		if !slices.Contains(names, env) {
			t.Errorf("config.gen.go reads %s, which Inputs does not name", env)
		}
	}
	var url *configgen.Input
	for i := range inputs {
		if inputs[i].Env == "APP_IDENTITY_ACCESS_URL" {
			url = &inputs[i]
		}
	}
	if url == nil || url.Substrate != "external-http" || !url.RequiredFor("local") {
		t.Fatalf("the ExternalHTTP endpoint must be a required local input, got %+v", url)
	}
	if !strings.Contains(src, `"local": {`) || !strings.Contains(src, `"APP_IDENTITY_ACCESS_URL"`) {
		t.Errorf("config.gen.go must require the endpoint:\n%s", src)
	}
}

// A hand hook may read only the variables the Config declares (FIX-049): RUN-017's hooks.go
// read APP_IDENTITY_PROVIDER_URL, which no declaration names.
func TestUndeclaredEnvReadsRejectsAnInventedName(t *testing.T) {
	inputs, err := configgen.Inputs(freshModel(t), freshCfg)
	if err != nil {
		t.Fatal(err)
	}
	hooks := []byte(`package main

// APP_IN_A_COMMENT is not a read.
const identityProviderURLEnv = "APP_IDENTITY_PROVIDER_URL"

func f() string { return getenv("APP_IDENTITY_ACCESS_URL") + getenv("APP_DEV_PRINCIPAL") + "OTHER_NAME" + ` + "`APP_RAW_NAME`" + ` }
`)
	got, err := configgen.UndeclaredEnvReads(inputs, "APP", map[string][]byte{"cmd/server/hooks.go": hooks})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/server/hooks.go: APP_IDENTITY_PROVIDER_URL", "cmd/server/hooks.go: APP_RAW_NAME"}
	if !slices.Equal(got, want) {
		t.Fatalf("UndeclaredEnvReads = %q, want %q", got, want)
	}
}
