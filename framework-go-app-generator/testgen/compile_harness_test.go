package testgen

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The harness compiles (and runs) generated test files in a throwaway module: the real framework-go,
// a stub scenario host (the real one boots Temporal and Postgres; a generated file's assertions do not need
// them) and whatever component stubs the test writes. It runs offline against the module cache.

// stubScenariohost is the part of the scenario host a generated file touches, minus the substrates. Its
// method set mirrors scenariohost's (host.go, callcontext.go, arrange.go); TestGo_TypeChecksAgainstTheRealHost
// holds the two together.
const stubScenariohost = `package scenariohost

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	fwe"github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

type Host struct{}
type Principal struct {
	Subject    string
	Roles      []string
	OnBehalfOf string
}
type Worker interface{}
type Workflows struct{ Register func(Worker) }

func Start(t *testing.T) *Host                                              { return &Host{} }
func Main(m *testing.M, components ...string)                               { os.Exit(m.Run()) }
func MainWithWorkflows(m *testing.M, wf Workflows, components ...string)    { os.Exit(m.Run()) }
func (h *Host) RunScenario(t *testing.T, component, id string, fn func(t *testing.T)) { fn(t) }
func (h *Host) Context(t *testing.T) context.Context                        { return t.Context() }
func (h *Host) PrincipalContext(t *testing.T, p Principal) context.Context {
	return security.WithPrincipal(t.Context(), security.Principal{Kind: security.PrincipalUser, Subject: p.Subject, Roles: p.Roles})
}
func (h *Host) ResourceAccessContextAs(t *testing.T, p Principal, key string) fwra.Context {
	return fwra.Context{Context: h.PrincipalContext(t, p), IdempotencyKey: fwra.IdempotencyKey(key)}
}
func (h *Host) EngineContextAs(t *testing.T, p Principal) fwe.Context {
	return fwe.Context{Context: h.PrincipalContext(t, p)}
}
func (h *Host) ManagerContextAs(t *testing.T, p Principal) fwm.Context {
	return fwm.Context{Context: h.PrincipalContext(t, p)}
}
func (h *Host) ResourceAccessContext(t *testing.T, key string) fwra.Context {
	return fwra.Context{Context: t.Context(), IdempotencyKey: fwra.IdempotencyKey(key)}
}
func (h *Host) EngineContext(t *testing.T) fwe.Context   { return fwe.Context{Context: t.Context()} }
func (h *Host) ManagerContext(t *testing.T) fwm.Context  { return fwm.Context{Context: t.Context()} }
// ProbeCalls mirrors scenariohost.ProbeCalls; the stub's probe deadline is 200ms (STUB_PROBE_DEADLINE
// overrides it), so a test of a hung read does not wait ten seconds.
type ProbeCalls struct{ ctx context.Context }

func (h *Host) ProbeContext(t *testing.T) ProbeCalls {
	d := 200 * time.Millisecond
	if v, err := time.ParseDuration(os.Getenv("STUB_PROBE_DEADLINE")); err == nil {
		d = v
	}
	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)
	return ProbeCalls{ctx}
}
func (p ProbeCalls) Context(t *testing.T) context.Context { return p.ctx }
func (p ProbeCalls) PrincipalContext(t *testing.T, pr Principal) context.Context {
	return security.WithPrincipal(p.ctx, security.Principal{Kind: security.PrincipalUser, Subject: pr.Subject, Roles: pr.Roles})
}
func (p ProbeCalls) ResourceAccessContext(t *testing.T, key string) fwra.Context {
	return fwra.Context{Context: p.ctx, IdempotencyKey: fwra.IdempotencyKey(key)}
}
func (p ProbeCalls) ResourceAccessContextAs(t *testing.T, pr Principal, key string) fwra.Context {
	return fwra.Context{Context: p.PrincipalContext(t, pr), IdempotencyKey: fwra.IdempotencyKey(key)}
}
func (p ProbeCalls) EngineContext(t *testing.T) fwe.Context { return fwe.Context{Context: p.ctx} }
func (p ProbeCalls) EngineContextAs(t *testing.T, pr Principal) fwe.Context {
	return fwe.Context{Context: p.PrincipalContext(t, pr)}
}
func (p ProbeCalls) ManagerContext(t *testing.T) fwm.Context { return fwm.Context{Context: p.ctx} }
func (p ProbeCalls) ManagerContextAs(t *testing.T, pr Principal) fwm.Context {
	return fwm.Context{Context: p.PrincipalContext(t, pr)}
}
func (h *Host) AdvanceClock(t *testing.T, d time.Duration) {}
func (h *Host) FailNext(t *testing.T, component, op, errorCode string) {}
func (h *Host) SeedResource(t *testing.T, resource string, fixture json.RawMessage) {}
`

// moduleOpts shapes the throwaway module.
type moduleOpts struct {
	mcp      bool // add the MCP SDK
	realHost bool // build against the real scenario host (type-checking only: running it boots Temporal)
}

// writeModule lays out a module example.com/app/server holding the given files (module-relative paths) beside
// the stub scenario host, and returns its root.
func writeModule(t *testing.T, files map[string]string, opts moduleOpts) string {
	t.Helper()
	root := t.TempDir()
	platform, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var gomod strings.Builder
	gomod.WriteString("module example.com/app/server\n\ngo 1.25\n\nrequire (\n" +
		"\tgithub.com/mixofreality-studio/archistrator-platform/framework-go v0.0.0\n" +
		"\tgithub.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost v0.0.0\n")
	if opts.mcp {
		gomod.WriteString("\tgithub.com/modelcontextprotocol/go-sdk v1.6.1\n")
	}
	const base = "github.com/mixofreality-studio/archistrator-platform/"
	gomod.WriteString(")\n\nreplace " + base + "framework-go => " + filepath.Join(platform, "framework-go") + "\n")
	all := map[string]string{}
	sumFrom := []string{"framework-go-app-generator", "framework-go-mcp-generator"}
	if opts.realHost {
		gomod.WriteString("replace " + base + "framework-go-scenariohost => " + filepath.Join(platform, "framework-go-scenariohost") + "\n")
		for _, dep := range []string{"framework-go-infrastructure-github", "framework-go-infrastructure-postgres", "framework-go-infrastructure-temporal"} {
			gomod.WriteString("replace " + base + dep + " => " + filepath.Join(platform, dep) + "\n")
		}
		sumFrom = append(sumFrom, "framework-go-scenariohost")
	} else {
		gomod.WriteString("replace " + base + "framework-go-scenariohost => ./stub/scenariohost\n")
		all["stub/scenariohost/go.mod"] = "module " + base + "framework-go-scenariohost\n\ngo 1.25\n\nrequire " + base + "framework-go v0.0.0\n"
		all["stub/scenariohost/scenariohost.go"] = stubScenariohost
	}
	all["go.mod"] = gomod.String()
	maps.Copy(all, files)
	for p, body := range all {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var sums []byte
	for _, mod := range sumFrom {
		raw, err := os.ReadFile(filepath.Join(platform, mod, "go.sum"))
		if err != nil {
			t.Fatal(err)
		}
		sums = append(sums, raw...)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), sums, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// goIn runs the go tool in root, offline, and returns its combined output.
func goIn(root string, env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off"), env...)
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}
