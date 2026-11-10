package testgen

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
)

// The machine clients run over the wire (Plan 3 Task 3.3): the generated test launches the built app
// (LaunchDevstack) and its subjects are sessions on the launched server, as the plan's principals.
// These tests read the generated Go of the todomvc conformance fixture, whose todoMcpServer (MCP)
// and todoApi (REST) clients carry the plans.

func todomvcOutput(t *testing.T) Output {
	t.Helper()
	out, err := Generate(testfixtures.Fixture(t, "todomvc"), Config{ModulePath: "example.com/todomvc"})
	if err != nil {
		t.Fatalf("Generate(todomvc): %v", err)
	}
	return out
}

// clientFile returns the generated machine-client file under the package suffix, whatever the leaf the
// fixture's contract names.
func clientFile(t *testing.T, out Output, goPkgSuffix string) string {
	t.Helper()
	for p, src := range out.Generated {
		if strings.HasSuffix(p, "/client_scenarios.gen_test.go") && strings.Contains(p, goPkgSuffix) {
			return string(src)
		}
	}
	t.Fatalf("no machine client file under %q; have %v", goPkgSuffix, keys(out.Generated))
	return ""
}

// An MCP client's subject is a session on the launched server: the app is launched once per test with
// every principal the plan declares allowlisted, and each actor's steps call through a session as that
// principal. Nothing is served in-process.
func TestMCP_OverWire_SubjectIsASession(t *testing.T) {
	src := clientFile(t, todomvcOutput(t), "todomcpserver")
	for _, want := range []string{
		`app := h.LaunchDevstack(t, "", principal("team-agent"), principal("team-member"))`,
		`subjectAsTeam_member := scenariohost.MCPSession(t, app, principal("team-member"))`,
		`.Call(h.Context(t), "`,
		"func errorHasCode(err error, code string) bool {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	for _, bad := range []string{
		"serveTools", "toolSession", "toolRegistrar", "scenariocompose.NewTodoMcpServer", "mcp.NewInMemoryTransports",
		"modelcontextprotocol", "security.WithPrincipal", "handlers :=", "handlers)",
	} {
		if strings.Contains(src, bad) {
			t.Errorf("an MCP client file must not carry %q:\n%s", bad, src)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "client.go", src, parser.AllErrors); err != nil {
		t.Fatalf("does not parse: %v", err)
	}
}

// A REST client reaches its routes through the same launched server; the table of routes is the
// planner's, typed as the host's Route.
func TestREST_OverWire_UsesRoutesTable(t *testing.T) {
	src := clientFile(t, todomvcOutput(t), "web/list")
	for _, want := range []string{
		`app := h.LaunchDevstack(t, "", principal("team-agent"))`,
		`subjectAsTeam_agent := scenariohost.RESTClient(t, app, principal("team-agent"), clientRoutes)`,
		"var clientRoutes = map[string]scenariohost.Route{",
		`"listAddTodo":`,
		`{Verb: "POST", Path: "`,
		"var restWireCodes = map[string]string{",
		"func errorHasCode(err error, code string) bool {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	for _, bad := range []string{
		"serveTools", "toolSession", "httptest", "restRequest", "scenariocompose.NewTodoApi", "security.", "func restRequest",
		"securityImport",
	} {
		if strings.Contains(src, bad) {
			t.Errorf("a REST client file must not carry %q:\n%s", bad, src)
		}
	}
}

// Every machine client scenario is a Go run of the null-subject discrimination now that its subject
// is the launched server: the operation a step calls is nulled in the launched process
// (AIARCH_NULL_OPS, inherited), named as the Manager's own "<Iface>.<Op>" the composed server's
// applyNullOps matches. None is skipped for being a client.
func TestDiscriminate_ListsClientScenarios(t *testing.T) {
	out := todomvcOutput(t)
	runs := map[string]int{}
	for _, d := range out.Discriminate {
		if d.Component != "todoApi" && d.Component != "todoMcpServer" {
			continue
		}
		runs[d.Component]++
		if !strings.HasSuffix(d.Package, "/list") || !strings.HasPrefix(d.NullOp, "ListManager.") {
			t.Errorf("a client run nulls the Manager's operation, got %+v", d)
		}
		if !strings.HasPrefix(d.Test, "TestScenario_") {
			t.Errorf("run %+v names no scenario test", d)
		}
	}
	for _, comp := range []string{"todoApi", "todoMcpServer"} {
		if runs[comp] == 0 {
			t.Errorf("%s has no discrimination run", comp)
		}
	}
	for _, s := range out.Skipped {
		if strings.Contains(s.Reason, "machine client") {
			t.Errorf("a machine client scenario is not skipped: %+v", s)
		}
	}
}

// No emitter writes a policy that permits everything: not the test generator, not the composition
// root's, not the null-subject one's. A scenario that passes because the policy lets it is a
// scenario that proves nothing about the app's authorization (FIX-047).
func TestEmitters_NoPermitAll(t *testing.T) {
	var offenders []string
	for _, dir := range []string{".", "../composegen", "../nullgen"} {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p) // #nosec G304 -- the generator's own sources
			if err != nil {
				return err
			}
			if strings.Contains(string(src), "permitAll") {
				offenders = append(offenders, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(offenders)
	if len(offenders) > 0 {
		t.Errorf("emitters that spell a permit-all policy: %v", offenders)
	}
}
