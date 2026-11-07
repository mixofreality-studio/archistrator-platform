package scenariohost

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// recorder stands in for *testing.T so a test can observe the fatal and skip
// outcomes Input produces without ending itself. Fatalf and Skip unwind with a
// panic, as the real ones unwind with runtime.Goexit.
type recorder struct {
	fatal, skip string
	cleanups    []func()
}

type unwind struct{}

func (r *recorder) Helper()                   {}
func (r *recorder) Cleanup(f func())          { r.cleanups = append(r.cleanups, f) }
func (r *recorder) Fatalf(f string, a ...any) { r.fatal = fmt.Sprintf(f, a...); panic(unwind{}) }
func (r *recorder) Skip(a ...any)             { r.skip = fmt.Sprint(a...); panic(unwind{}) }

// run calls f, absorbing the unwind a Fatalf or Skip raises, then runs the cleanups.
func (r *recorder) run(f func()) {
	defer func() {
		for i := len(r.cleanups) - 1; i >= 0; i-- {
			r.cleanups[i]()
		}
		if p := recover(); p != nil {
			if _, ok := p.(unwind); !ok {
				panic(p)
			}
		}
	}()
	f()
}

func inputHost() *Host {
	return &Host{TemporalHostPort: "127.0.0.1:7233", TemporalNamespace: "aiarch-test", PostgresURL: "postgres://aiarch@127.0.0.1/aiarch_test"}
}

// TestInput_EverySubstrateHasAnArm walks the whole catalog: every input of every substrate
// either resolves to a value, skips (Postgres outside -short), or fails naming the double the
// host does not have. None may fall through as an unknown substrate or input, so a substrate
// added to the catalog without an arm here turns this test red.
func TestInput_EverySubstrateHasAnArm(t *testing.T) {
	wantValue := map[projectmodel.Substrate][]string{
		projectmodel.SubstrateTemporal: {"HOSTPORT", "NAMESPACE"},
		projectmodel.SubstratePostgres: {"URL"},
		projectmodel.SubstrateGitRepo:  {"URL"},
	}
	for _, s := range projectmodel.Substrates() {
		for _, in := range s.Inputs() {
			t.Run(string(s)+"/"+in.Name, func(t *testing.T) {
				h := inputHost()
				r := &recorder{}
				var got string
				r.run(func() { got = h.input(r, "k", s, in.Name) })
				if strings.Contains(r.fatal, "unknown") {
					t.Fatalf("no arm for substrate %q input %q: %s", s, in.Name, r.fatal)
				}
				if slices.Contains(wantValue[s], in.Name) {
					if r.fatal != "" || r.skip != "" || got == "" {
						t.Fatalf("want a value, got %q fatal=%q skip=%q", got, r.fatal, r.skip)
					}
					return
				}
				if !strings.Contains(r.fatal, "no "+string(s)+" double") {
					t.Fatalf("an input the host cannot back must name the missing double, got fatal=%q value=%q", r.fatal, got)
				}
			})
		}
	}
}

func TestInput_Values(t *testing.T) {
	h := inputHost()
	r := &recorder{}
	r.run(func() {
		if got := h.input(r, "temporal", projectmodel.SubstrateTemporal, "HOSTPORT"); got != "127.0.0.1:7233" {
			t.Errorf("HOSTPORT = %q", got)
		}
		if got := h.input(r, "temporal", projectmodel.SubstrateTemporal, "NAMESPACE"); got != "aiarch-test" {
			t.Errorf("NAMESPACE = %q", got)
		}
		if got := h.input(r, "pg", projectmodel.SubstratePostgres, "URL"); got != h.PostgresURL {
			t.Errorf("postgres URL = %q", got)
		}
		if got := h.input(r, "repo", projectmodel.SubstrateGitRepo, "URL"); !strings.HasPrefix(got, "file://") {
			t.Errorf("git-repo URL = %q, want the file:// URL of a bare repo", got)
		}
	})
}

func TestInput_PostgresIsSkippedWhenNotBooted(t *testing.T) {
	h := inputHost()
	h.PostgresURL = ""
	r := &recorder{}
	r.run(func() { h.input(r, "pg", projectmodel.SubstratePostgres, "URL") })
	if r.skip == "" || r.fatal != "" {
		t.Fatalf("want a skip, got skip=%q fatal=%q", r.skip, r.fatal)
	}
}

func TestInput_UnbackedSubstrateNamesTheMissingDouble(t *testing.T) {
	for _, s := range []projectmodel.Substrate{projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateExternalHTTP} {
		h := inputHost()
		r := &recorder{}
		in := s.Inputs()[0].Name
		r.run(func() { h.input(r, "the-key", s, in) })
		for _, want := range []string{`"the-key"`, string(s), in, "no " + string(s) + " double"} {
			if !strings.Contains(r.fatal, want) {
				t.Errorf("%s: fatal %q does not name %q", s, r.fatal, want)
			}
		}
	}
}

func TestInput_OtelDeclaresNoInput(t *testing.T) {
	h := inputHost()
	r := &recorder{}
	r.run(func() { h.input(r, "otel", projectmodel.SubstrateOtel, "URL") })
	if !strings.Contains(r.fatal, "otel") {
		t.Fatalf("want a fatal naming otel, got %q", r.fatal)
	}
}

// TestGitRepo_OnePerKey: a key gets one bare repo however often it is asked for, two keys
// get two, and a repo is a real on-disk bare repository.
func TestGitRepo_OnePerKey(t *testing.T) {
	h := inputHost()
	r := &recorder{}
	t.Cleanup(h.stopAll)
	r.run(func() {
		a1 := h.gitRepo(r, "project-state-git-repo")
		a2 := h.gitRepo(r, "project-state-git-repo")
		b := h.gitRepo(r, "artifact-repo")
		if a1 != a2 {
			t.Errorf("one repo per key: %+v vs %+v", a1, a2)
		}
		if a1.URL == b.URL || a1.Dir == b.Dir {
			t.Errorf("two keys must get two repos: %+v vs %+v", a1, b)
		}
		assertRealGitRepoWithTip(t, a1.URL, "main")
	})
}

// TestEnv_LocalInfraOnly: Env lists one NAME=VALUE per input of every infra provisioned for
// the local profile, named by InfraEnvName (override first), and nothing for infra that is
// cloud-only, however the host could not back it.
func TestEnv_LocalInfraOnly(t *testing.T) {
	h := inputHost()
	t.Cleanup(h.stopAll)
	d := &projectmodel.Deployment{Infrastructure: []projectmodel.InfraDecl{
		{Key: "temporal", Substrate: projectmodel.SubstrateTemporal, Profiles: []string{"cloud", "local"}, Presence: "required"},
		{Key: "postgres", Substrate: projectmodel.SubstratePostgres, Profiles: []string{"local"}, Presence: "required"},
		{Key: "project-state-git-repo", Substrate: projectmodel.SubstrateGitRepo, Profiles: []string{"local"}, Presence: "required",
			Env: map[string]string{"URL": "ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL"}},
		{Key: "otel", Substrate: projectmodel.SubstrateOtel, Profiles: []string{"cloud", "local"}, Presence: "optional"},
		{Key: "keycloak", Substrate: projectmodel.SubstrateKeycloak, Profiles: []string{"cloud"}, Presence: "required"},
	}}
	got := h.Env(t, "APP", d)
	want := []string{
		"APP_TEMPORAL_HOSTPORT=127.0.0.1:7233",
		"APP_TEMPORAL_NAMESPACE=aiarch-test",
		"APP_POSTGRES_URL=" + h.PostgresURL,
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("Env is missing %q in %v", w, got)
		}
	}
	var repoEnv string
	for _, e := range got {
		switch {
		case strings.HasPrefix(e, "ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL="):
			repoEnv = e
		case strings.Contains(e, "KEYCLOAK"), strings.Contains(e, "OTEL"):
			t.Errorf("Env carries %q: a cloud-only or input-less infra contributes nothing", e)
		}
	}
	if !strings.HasPrefix(repoEnv, "ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file://") {
		t.Errorf("git-repo env must use the declared override and the repo URL, got %q in %v", repoEnv, got)
	}
	if len(got) != 4 {
		t.Errorf("want 4 entries (2 temporal, postgres, git-repo), got %v", got)
	}
}
