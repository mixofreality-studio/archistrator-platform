package rungen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/rungen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// gitRepoProject declares two local git-repo infrastructure inputs, one required and one
// optional, and nothing else: no Manager, so no Temporal.
const gitRepoProject = `{"slots":{"6":{"kind":6,"model":{"deployment":{
	"deliveryStyle":"local",
	"environments":[{"profile":"cloud"},{"profile":"local"}],
	"containers":[{"key":"server","name":"server","components":[]}],
	"infrastructure":[
		{"key":"project-state-git-repo","substrate":"git-repo","profiles":["local"],"presence":"required"},
		{"key":"artifact-repo","substrate":"git-repo","profiles":["local"],"presence":"optional"}],
	"settings":[{"name":"listenAddr","type":"string","default":":8080"}]}}}}}`

func gitRepoModel(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.Load([]byte(gitRepoProject))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The launcher provisions a temporary bare repo for every local git-repo key, as it does Temporal
// and Postgres, and sets the key's URL variable from it. The URL is never required from the
// caller: a git-repo is a double the launcher can make.
func TestRungen_ProvisionsGitRepo(t *testing.T) {
	m := gitRepoModel(t)
	p, err := rungen.Derive(m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"artifact-repo", "project-state-git-repo"}; !slices.Equal(p.GitRepos, want) {
		t.Fatalf("GitRepos = %v, want %v", p.GitRepos, want)
	}
	for env, key := range map[string]string{
		"APP_PROJECT_STATE_GIT_REPO_URL": "project-state-git-repo",
		"APP_ARTIFACT_REPO_URL":          "artifact-repo",
	} {
		want := rungen.ProvisionedEnv{Env: env, From: rungen.GitRepoURL(key)}
		if !slices.Contains(p.Provisioned, want) {
			t.Errorf("Provisioned lacks %+v: %+v", want, p.Provisioned)
		}
		if slices.Contains(p.Required, env) {
			t.Errorf("%s is provisioned, so it must not be required of the caller", env)
		}
	}
	if p.Temporal || p.Postgres {
		t.Errorf("only git repos were declared: %+v", p)
	}
	src := generate(t, m)
	for _, want := range []string{`GitRepos:`, `"project-state-git-repo"`, `From: "git-repo.project-state-git-repo.url"`} {
		if !strings.Contains(src, want) {
			t.Errorf("the launcher lacks %q:\n%s", want, src)
		}
	}
}

// The launcher that provisions git repos still builds and vets as a stdlib-only command.
func TestRungen_GitRepoLauncherBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a sandbox module")
	}
	dir := t.TempDir()
	src := generate(t, gitRepoModel(t))
	for name, content := range map[string]string{
		"go.mod":                                 "module example.com/gitrepo\n\ngo 1.25\n",
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
