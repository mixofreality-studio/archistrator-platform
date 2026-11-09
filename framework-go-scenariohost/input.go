package scenariohost

import (
	"testing"

	ghtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The one infra-to-substrate mapping (Plan 2 Task 2.11c). A deployment model declares
// infrastructure (a key, a catalog substrate, the inputs that substrate takes); the host is
// what backs it in a test. The generated scenario subject asks the host for each input it needs
// (Input), and the local-profile launch asks for all of them at once (Env), so there is exactly
// one place that says which double answers which declared input.

// reporter is the part of *testing.T the mapping uses. A *testing.T satisfies it; the tests of
// the mapping satisfy it with a recorder so a fatal outcome can be observed.
type reporter interface {
	Helper()
	Fatalf(format string, args ...any)
	Skip(args ...any)
}

// Input is the value the host supplies for one input of the infrastructure declared under
// infraKey, whose substrate is s: the Temporal dev server's address or namespace, the Postgres
// container's URL (the test is skipped where Postgres is not booted), the URL of the bare git
// repository kept for infraKey (GitRepo). A substrate the host has no double for (github-app,
// keycloak, external-http) fails the test naming the key, the substrate and the input: a scenario
// that needs one is not yet expressible, and it says so rather than running against an empty
// string. otel takes no input.
func (h *Host) Input(t *testing.T, infraKey string, s projectmodel.Substrate, input string) string {
	t.Helper()
	return h.input(t, infraKey, s, input)
}

func (h *Host) input(t reporter, infraKey string, s projectmodel.Substrate, input string) string {
	t.Helper()
	switch s {
	case projectmodel.SubstrateTemporal:
		if v, ok := h.temporalInput(input); ok {
			return v
		}
	case projectmodel.SubstratePostgres:
		if input == "URL" {
			if h.PostgresURL == "" {
				t.Skip("scenariohost: postgres is not booted under -short")
			}
			return h.PostgresURL
		}
	case projectmodel.SubstrateGitRepo:
		if input == "URL" {
			return h.gitRepo(t, infraKey).URL
		}
	case projectmodel.SubstrateOtel:
		t.Fatalf("scenariohost: infra %q (%s) declares no input, asked for %q", infraKey, s, input)
		return ""
	case projectmodel.SubstrateGitHubApp, projectmodel.SubstrateKeycloak, projectmodel.SubstrateExternalHTTP:
		t.Fatalf("scenariohost: infra %q (%s) input %s: the host has no %s double", infraKey, s, input, s)
		return ""
	}
	t.Fatalf("scenariohost: infra %q: unknown substrate %q or input %q", infraKey, s, input)
	return ""
}

// temporalInput is the Temporal dev server's address or namespace.
func (h *Host) temporalInput(input string) (string, bool) {
	switch input {
	case "HOSTPORT":
		return h.TemporalHostPort, true
	case "NAMESPACE":
		return h.TemporalNamespace, true
	}
	return "", false
}

// GitRepo is the bare git repository the host keeps for infraKey: one per key, created on first
// use with an initial commit on main, and removed with the host. Two keys never share a
// repository, so two declared repositories (the project state and an artifact store, say) cannot
// see each other's commits.
func (h *Host) GitRepo(t *testing.T, infraKey string) ghtestinfra.LocalGitRepo {
	t.Helper()
	return h.gitRepo(t, infraKey)
}

func (h *Host) gitRepo(t reporter, infraKey string) ghtestinfra.LocalGitRepo {
	t.Helper()
	h.gitMu.Lock()
	defer h.gitMu.Unlock()
	if repo, ok := h.gitRepos[infraKey]; ok {
		return repo
	}
	repo, stop, err := startLocalGitRepo("main")
	if err != nil {
		t.Fatalf("scenariohost: git repository for infra %q: %v", infraKey, err)
		return ghtestinfra.LocalGitRepo{}
	}
	if h.gitRepos == nil {
		h.gitRepos = map[string]ghtestinfra.LocalGitRepo{}
	}
	h.gitRepos[infraKey] = repo
	h.stops = append(h.stops, stop)
	return repo
}

// Env is the environment the local run contract launches an app with: for every infrastructure
// the deployment provisions for the local profile, one NAME=VALUE per input of its substrate,
// NAME from projectmodel.InfraEnvName under prefix and VALUE from Input. Infrastructure that is
// not provisioned locally contributes nothing.
func (h *Host) Env(t *testing.T, prefix string, d *projectmodel.Deployment) []string {
	t.Helper()
	return h.env(prefix, d, func(decl projectmodel.InfraDecl, input string) string {
		return h.Input(t, decl.Key, decl.Substrate, input)
	})
}

// env is Env with the value of each input answered by value: Env answers from Input, and
// LaunchDevstack answers a database and a namespace of the launch's own.
func (h *Host) env(prefix string, d *projectmodel.Deployment, value func(decl projectmodel.InfraDecl, input string) string) []string {
	var out []string
	for _, decl := range d.Infrastructure {
		if !provisionedLocally(decl) {
			continue
		}
		for _, in := range decl.Substrate.Inputs() {
			out = append(out, projectmodel.InfraEnvName(prefix, decl, in.Name)+"="+value(decl, in.Name))
		}
	}
	return out
}

func provisionedLocally(decl projectmodel.InfraDecl) bool {
	for _, p := range decl.Profiles {
		if p == projectmodel.LocalProfile {
			return true
		}
	}
	return false
}

// stopAll tears down what the host booted, in reverse order of boot. Main does it at the end of
// the process; a test that builds a bare Host does it at the end of the test.
func (h *Host) stopAll() {
	for i := len(h.stops) - 1; i >= 0; i-- {
		h.stops[i]()
	}
	h.stops = nil
}
