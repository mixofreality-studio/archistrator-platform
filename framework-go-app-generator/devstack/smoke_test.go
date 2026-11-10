package devstack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The todomvc fixture's MCP client fronts the list Manager, so the tools a booted server must
// list are the Manager-namespaced operations: the same expression mcpgen registers them by.
func TestToolNames_MatchesTheGeneratedTables(t *testing.T) {
	m, err := projectmodel.Load(testfixtures.Fixture(t, "todomvc"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"listAddTodo", "listClearCompleted", "listDiscardTodo", "listMarkTodo", "listOpenList", "listRewordTodo", "listToggleAll"}
	if got := ToolNames(m, nil); !slices.Equal(got, want) {
		t.Errorf("ToolNames = %v, want %v", got, want)
	}
	// An exposed-ops override narrows a Manager to the operations its transports really serve.
	got := ToolNames(m, map[string][]string{"listManager": {"AddTodo", "OpenList"}})
	if want := []string{"listAddTodo", "listOpenList"}; !slices.Equal(got, want) {
		t.Errorf("ToolNames(exposed) = %v, want %v", got, want)
	}
	// A model with no MCP client expects no tool.
	if got := ToolNames(&projectmodel.Model{}, nil); len(got) != 0 {
		t.Errorf("ToolNames(empty model) = %v, want none", got)
	}
}

type noArgs struct{}
type noOut struct{}

// fakeApp serves /readyz, / (the index, when non-empty) and a streamable MCP server registering
// tools at /mcp.
func fakeApp(t *testing.T, index string, tools ...string) *App {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)
	for _, name := range tools {
		mcp.AddTool(srv, &mcp.Tool{Name: name, Description: name}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, noOut, error) {
			return &mcp.CallToolResult{}, noOut{}, nil
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if index == "" || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(index))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &App{BaseURL: ts.URL, MCPURL: ts.URL + "/mcp", Stop: func() error { return nil }, Log: func() string { return "" }}
}

func smokeCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// A server whose /mcp initializes but lists a tool table short of the model's (FIX-050) is not
// booted.
func TestSmoke_MCPListsEveryGeneratedTool(t *testing.T) {
	app := fakeApp(t, "<html>", "A", "B")
	if err := Smoke(smokeCtx(t), app, SmokeSpec{SPA: true, Tools: []string{"A", "B"}}); err != nil {
		t.Fatalf("Smoke over a complete table: %v", err)
	}
	err := Smoke(smokeCtx(t), app, SmokeSpec{SPA: true, Tools: []string{"A", "B", "C"}})
	if err == nil || !strings.Contains(err.Error(), "C") {
		t.Fatalf("Smoke with a missing tool C = %v, want an error naming it", err)
	}
	if err != nil && strings.Contains(err.Error(), "missing A") {
		t.Errorf("error names a tool that is listed: %v", err)
	}
}

func TestSmoke_FailsWhenSPAMissing(t *testing.T) {
	app := fakeApp(t, "", "A")
	err := Smoke(smokeCtx(t), app, SmokeSpec{SPA: true, Tools: []string{"A"}})
	if err == nil || !strings.Contains(err.Error(), "GET / = 404") {
		t.Fatalf("Smoke without an index = %v, want GET / = 404", err)
	}
	if err := Smoke(smokeCtx(t), app, SmokeSpec{SPA: false, Tools: []string{"A"}}); err != nil {
		t.Errorf("SPA:false must not look at /: %v", err)
	}
	notHTML := fakeApp(t, "just text", "A")
	if err := Smoke(smokeCtx(t), notHTML, SmokeSpec{SPA: true, Tools: []string{"A"}}); err == nil || !strings.Contains(err.Error(), "<html") {
		t.Errorf("Smoke over a non-HTML index = %v, want a no-<html error", err)
	}
}

func TestSmoke_FailsWhenToolsEmpty(t *testing.T) {
	app := fakeApp(t, "<html>")
	err := Smoke(smokeCtx(t), app, SmokeSpec{Tools: []string{"A"}})
	if err == nil || !strings.Contains(err.Error(), "tools/list returned no tools") {
		t.Fatalf("Smoke over an empty tool table = %v, want tools/list returned no tools", err)
	}
	// No tool expected while the app mounts /mcp is a spec the caller got wrong.
	mounted := fakeApp(t, "<html>", "A")
	err = Smoke(smokeCtx(t), mounted, SmokeSpec{})
	if err == nil || !strings.Contains(err.Error(), "derive the list with ToolNames") {
		t.Fatalf("Smoke with MCP mounted and no tool expected = %v, want a ToolNames hint", err)
	}
}

func TestSmoke_ReadyzMustAnswer200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer ts.Close()
	err := Smoke(smokeCtx(t), &App{BaseURL: ts.URL, MCPURL: ts.URL + "/mcp"}, SmokeSpec{})
	if err == nil || !strings.Contains(err.Error(), "/readyz") {
		t.Fatalf("Smoke over an unready app = %v, want a /readyz error", err)
	}
}
