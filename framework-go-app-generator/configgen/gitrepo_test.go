package configgen_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

const gitRepoDeployment = `{"slots":{"6":{"kind":6,"model":{"deployment":{
	"deliveryStyle":"local",
	"environments":[{"profile":"cloud"},{"profile":"local"}],
	"containers":[{"key":"server","name":"server","components":[]}],
	"infrastructure":[{"key":"project-state-git-repo","substrate":"git-repo","profiles":["local"],"presence":"required"}],
	"settings":[{"name":"listenAddr","type":"string","default":":8080"}]}}}}}`

// A git-repo infrastructure declaration surfaces as a ProjectStateGitRepoURL field read from
// <PREFIX>_PROJECT_STATE_GIT_REPO_URL, and the output is pinned by a golden.
func TestGitRepoGolden(t *testing.T) {
	m, err := projectmodel.Load([]byte(gitRepoDeployment))
	if err != nil {
		t.Fatal(err)
	}
	cfg := configgen.Config{ContainerKey: "server", EnvPrefix: "APP", PackageName: "config"}
	files, err := configgen.Generate(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	src := files["config.gen.go"]
	for _, want := range []string{"ProjectStateGitRepoURL", `"APP_PROJECT_STATE_GIT_REPO_URL"`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("config.gen.go lacks %q:\n%s", want, src)
		}
	}
	checkGolden(t, "../testdata/gitrepo.configgen.config.gen.go.golden", src)

	inputs, err := configgen.Inputs(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, in := range inputs {
		if in.Env == "APP_PROJECT_STATE_GIT_REPO_URL" {
			found = in.Field == "ProjectStateGitRepoURL" && in.Substrate == "git-repo" && in.CatalogInput == "URL" && in.RequiredFor("local")
		}
	}
	if !found {
		t.Errorf("Inputs lacks the git-repo URL input: %+v", inputs)
	}
}
