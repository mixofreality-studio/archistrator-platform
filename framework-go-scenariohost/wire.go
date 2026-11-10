package scenariohost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The machine clients of a generated scenario test (Plan 3 Task 3.3): a client scenario drives the
// app the test launched (LaunchDevstack) over the wire, as each of the plan's principals, through
// the same generated MCP tools or REST routes a real client would use. The launched app serves a
// request as the principal its X-Dev-Principal header names, provided the launch allowlisted it.

// devPrincipalHeader names the request header that picks a launched app's principal. It is the
// generated edge's (httpgen's DevPrincipalHeader), which this module does not import.
const devPrincipalHeader = "X-Dev-Principal"

// Session is a client's connection to the launched app as one principal: the subject a generated
// machine-client scenario calls. Call invokes the operation called name (the MCP tool, or the key
// of the REST route table) with args, a JSON object, and returns its decoded result. A refusal or
// failure of any kind is the call's error, carrying the server's text.
type Session interface {
	Call(ctx context.Context, name, args string) (any, error)
}

// Route is the generated route of one operation: its HTTP verb and path, in which {param} is
// replaced by the argument of that name.
type Route struct{ Verb, Path string }

// devPrincipalTransport sends every request as subject. An empty subject sends no header, so the
// app serves the request as its own process principal.
type devPrincipalTransport struct{ subject string }

func (d devPrincipalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if d.subject == "" {
		return http.DefaultTransport.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set(devPrincipalHeader, d.subject)
	return http.DefaultTransport.RoundTrip(r)
}

// MCPSession connects an MCP client to app's /mcp as p and returns it as a Session; Call is
// tools/call, and a result flagged isError is the call's error carrying the tool's text. The
// session closes when t ends. A connection the app refuses (a subject the launch did not allowlist
// is a 401) fails the test; TryMCPSession returns it instead.
func MCPSession(t *testing.T, app *App, p Principal) Session {
	t.Helper()
	s, err := TryMCPSession(t, app, p)
	if err != nil {
		t.Fatalf("scenariohost: MCP session as %q: %v\n%s", p.Subject, err, app.Log())
	}
	return s
}

// TryMCPSession is MCPSession returning a refused connection as an error.
func TryMCPSession(t *testing.T, app *App, p Principal) (Session, error) {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint:   app.MCPURL,
		HTTPClient: &http.Client{Transport: devPrincipalTransport{subject: p.Subject}},
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "scenario", Version: "testgen"}, nil).Connect(t.Context(), transport, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = cs.Close() })
	return mcpSession{cs: cs}, nil
}

type mcpSession struct{ cs *mcp.ClientSession }

// Call calls tool name with args (a JSON object) and returns its structured output. A protocol
// error (an unknown tool, arguments the tool's input schema rejects) and a result flagged isError
// are both the call's error, carrying the tool's text.
func (s mcpSession) Call(ctx context.Context, name, args string) (any, error) {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		var text []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text = append(text, tc.Text)
			}
		}
		return nil, errors.New(name + ": " + strings.Join(text, "\n"))
	}
	return res.StructuredContent, nil
}

// RESTClient is a REST client of app as p: Call requests the operation's route from routes with
// args (a JSON object). A {param} in the path takes its value from args, and the rest is the query
// of a GET or the JSON body of anything else. A status of 400 or more is the call's error,
// "<name>: <status> <body>"; a body that is empty is a nil result.
func RESTClient(t *testing.T, app *App, p Principal, routes map[string]Route) Session {
	t.Helper()
	return restClient{
		base:   strings.TrimSuffix(app.BaseURL, "/"),
		routes: routes,
		http:   &http.Client{Transport: devPrincipalTransport{subject: p.Subject}},
	}
}

type restClient struct {
	base   string
	routes map[string]Route
	http   *http.Client
}

func (c restClient) Call(ctx context.Context, name, args string) (any, error) {
	route, ok := c.routes[name]
	if !ok {
		return nil, errors.New(name + ": no such route")
	}
	req, err := restRequest(ctx, route.Verb, c.base+route.Path, args)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("%s: %d %s", name, res.StatusCode, body)
	}
	if len(body) == 0 {
		return nil, nil
	}
	var out any
	return out, json.Unmarshal(body, &out)
}

// restRequest spells args onto the route.
func restRequest(ctx context.Context, verb, target, args string) (*http.Request, error) {
	fields := map[string]any{}
	dec := json.NewDecoder(strings.NewReader(args))
	dec.UseNumber() // a large integer stays an integer on the query, not 1e+06
	if err := dec.Decode(&fields); err != nil {
		return nil, err
	}
	target = fillPathHoles(target, fields)
	var body io.Reader
	if verb == http.MethodGet {
		target += queryOf(fields)
	} else {
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	return http.NewRequestWithContext(ctx, verb, target, body)
}

// fillPathHoles replaces each {name} of target by the field of that name, removing the field: an
// argument the path took is not sent again.
func fillPathHoles(target string, fields map[string]any) string {
	for name, value := range fields {
		if hole := "{" + name + "}"; strings.Contains(target, hole) {
			target = strings.ReplaceAll(target, hole, url.PathEscape(fmt.Sprint(value)))
			delete(fields, name)
		}
	}
	return target
}

// queryOf is the fields as the query of a GET ("" with none).
func queryOf(fields map[string]any) string {
	query := url.Values{}
	for name, value := range fields {
		query.Set(name, fmt.Sprint(value))
	}
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}
