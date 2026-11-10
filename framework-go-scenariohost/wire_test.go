package scenariohost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The unit half of the machine clients (Plan 3 Task 3.3): they are driven against stand-in servers
// that record what arrived, so no devstack boots. The over-the-wire half runs against the built
// fixture in framework-go-conformance.

// echoServer answers every request with a JSON line naming what it received, and answers a path
// ending in /missing with 404.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/missing") {
			http.Error(w, `{"code":"not_found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "body": string(body),
			"principal": r.Header.Get("X-Dev-Principal"), "hasHeader": len(r.Header.Values("X-Dev-Principal")) > 0,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

var testRoutes = map[string]Route{
	"listAddTodo":  {Verb: "POST", Path: "/api/v1/lists/{owner}/todos"},
	"listOpenList": {Verb: "GET", Path: "/api/v1/lists/{owner}"},
	"listGone":     {Verb: "GET", Path: "/api/v1/lists/{owner}/missing"},
}

func callREST(t *testing.T, s Session, name, args string) map[string]any {
	t.Helper()
	out, err := s.Call(t.Context(), name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("%s: result %T, want an object", name, out)
	}
	return m
}

// RESTClient spells a call's args onto its route as testgen's in-process client did: a {hole} in
// the path takes its value from args, a GET sends the rest as its query, anything else as the JSON
// body; the principal rides the X-Dev-Principal header.
func TestRESTClient_SpellsArgsOntoTheRoute(t *testing.T) {
	srv := echoServer(t)
	app := &App{BaseURL: srv.URL, MCPURL: srv.URL + "/mcp"}
	s := RESTClient(t, app, Principal{Subject: "alice"}, testRoutes)

	got := callREST(t, s, "listAddTodo", `{"owner":"alice","title":"milk","qty":1000000}`)
	if got["method"] != "POST" || got["path"] != "/api/v1/lists/alice/todos" || got["principal"] != "alice" {
		t.Errorf("POST: %v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got["body"].(string)), &body); err != nil || body["title"] != "milk" || body["owner"] != nil {
		t.Errorf("the body carries the args the path did not take: %v (%v)", got["body"], err)
	}

	got = callREST(t, s, "listOpenList", `{"owner":"a b","limit":1000000}`)
	if got["method"] != "GET" || got["path"] != "/api/v1/lists/a b" || got["query"] != "limit=1000000" || got["body"] != "" {
		t.Errorf("GET: %v", got)
	}
}

// A status of 400 or more is the call's error, "<name>: <status> <body>"; an unknown name is one too.
func TestRESTClient_StatusIsTheError(t *testing.T) {
	srv := echoServer(t)
	s := RESTClient(t, &App{BaseURL: srv.URL}, Principal{Subject: "alice"}, testRoutes)
	_, err := s.Call(t.Context(), "listGone", `{"owner":"alice"}`)
	if err == nil || !strings.Contains(err.Error(), "listGone: 404") || !strings.Contains(err.Error(), "not_found") {
		t.Errorf("want a 404 error naming the call and the body, got %v", err)
	}
	if _, err := s.Call(t.Context(), "nope", `{}`); err == nil || !strings.Contains(err.Error(), "no such route") {
		t.Errorf("an unknown route is an error, got %v", err)
	}
}

// A Principal with no subject sends no header, so the server answers as its own process principal.
func TestRESTClient_NoSubjectSendsNoHeader(t *testing.T) {
	srv := echoServer(t)
	s := RESTClient(t, &App{BaseURL: srv.URL}, Principal{}, testRoutes)
	got := callREST(t, s, "listOpenList", `{"owner":"alice"}`)
	if got["hasHeader"] != false {
		t.Errorf("a subject-less client sent the header: %v", got)
	}
}

// mcpEcho is a stand-in MCP server behind an HTTP handler that records the principal header of
// every request it served.
type mcpEcho struct {
	mu         sync.Mutex
	principals []string
}

func (m *mcpEcho) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "echo", Version: "test"}, nil)
	type in struct {
		Text string `json:"text"`
	}
	type out struct {
		Echo string `json:"echo"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo"}, func(_ context.Context, _ *mcp.CallToolRequest, a in) (*mcp.CallToolResult, out, error) {
		return nil, out{Echo: a.Text}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "boom"}, func(_ context.Context, _ *mcp.CallToolRequest, _ in) (*mcp.CallToolResult, out, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "NotFound: no such list"}}}, out{}, nil
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.principals = append(m.principals, r.Header.Get("X-Dev-Principal"))
		m.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// MCPSession connects as the principal (every request carries its header), Call returns the tool's
// structured output, and a result flagged isError is the call's error carrying the tool's text.
func TestMCPSession_CarriesThePrincipalAndMapsErrors(t *testing.T) {
	var rec mcpEcho
	srv := rec.serve(t)
	s := MCPSession(t, &App{BaseURL: srv.URL, MCPURL: srv.URL}, Principal{Subject: "alice"})

	out, err := s.Call(t.Context(), "echo", `{"text":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(out); got != "map[echo:hi]" {
		t.Errorf("structured output = %s, want map[echo:hi]", got)
	}
	_, err = s.Call(t.Context(), "boom", `{"text":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "boom: NotFound: no such list") {
		t.Errorf("want the tool's text in the error, got %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.principals) == 0 {
		t.Fatal("no request reached the server")
	}
	for i, p := range rec.principals {
		if p != "alice" {
			t.Errorf("request %d carried principal %q, want alice (every request, not just the first)", i, p)
		}
	}
}
