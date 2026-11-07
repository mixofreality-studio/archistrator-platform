package testgen

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// emit_rest.go is the REST machine client's dialect. Like an MCP client, every op is one generated
// surface entry, spelled ManagerBase+Op, and its scenario test drives that entry in-process through
// the generated handlers against the real components below: here over HTTP, there over an in-memory
// MCP transport. The two are separate tests of separate clients. The scenario bodies and the hooks
// are the same code as the MCP dialect's (emit_mcp.go): only the transport under toolSession differs.

// securityImport is the framework utility a generated REST handler authorizes through.
const securityImport = "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"

// restRoute is one generated route of a Manager.
type restRoute struct{ Verb, Path string }

// managerRoutes is every Manager operation's route, by its client op name. It reads the same route
// planner the handlers are generated from, so a test cannot name a route the handler does not mount.
func managerRoutes(projectJSON []byte) (map[string]restRoute, error) {
	var doc struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(projectJSON, &doc); err != nil {
		return nil, fmt.Errorf("testgen: %w", err)
	}
	routes := map[string]restRoute{}
	for key, raw := range doc.ServiceContracts {
		if err := addManagerRoutes(routes, raw); err != nil {
			return nil, fmt.Errorf("testgen: contract %s: %w", key, err)
		}
	}
	return routes, nil
}

// addManagerRoutes adds one contract's routes, when it is a Manager's.
func addManagerRoutes(routes map[string]restRoute, raw json.RawMessage) error {
	var meta struct {
		Layer     string `json:"layer"`
		Interface struct {
			Layer string `json:"layer"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	if !strings.EqualFold(meta.Layer, "manager") && !strings.EqualFold(meta.Interface.Layer, "manager") {
		return nil
	}
	parsed, err := projectmodel.Parse(raw)
	if err != nil {
		return err
	}
	plans, err := httpgen.PlanOps(parsed)
	if err != nil {
		return err
	}
	for _, p := range plans {
		routes[parsed.ManagerBase()+p.Name] = restRoute{Verb: p.Verb, Path: p.PathTemplate}
	}
	return nil
}

// transportHelpers is the code under toolSession for the plan's dialect.
func transportHelpers(plan componentPlan, rest bool, routes map[string]restRoute) (string, error) {
	if !rest {
		return toolsHelpers, nil
	}
	table, err := restRouteTable(plan, routes)
	if err != nil {
		return "", err
	}
	return table + restHelpers, nil
}

// errorHasCodeHelper is the transport's code match.
func errorHasCodeHelper(rest bool) string {
	if rest {
		return restErrorHasCode
	}
	return toolsErrorHasCode
}

func isRESTPlan(plan componentPlan) (bool, error) {
	d, err := clientDialect(plan.Key, plan.Contract)
	return d == dialectREST, err
}

func transportImports(rest bool) []string {
	if rest {
		return []string{securityImport, "bytes", "context", "encoding/json", "errors", "fmt", "io", "net", "net/http", "net/http/httptest", "net/url", "reflect", "strings"}
	}
	return []string{securityImport, "context", "encoding/json", "errors", "reflect", "strings", mcpSDKImport}
}

// restRouteTable is the generated map from tool name to route, over the plan's contract.
func restRouteTable(plan componentPlan, routes map[string]restRoute) (string, error) {
	lines := make([]string, 0, len(plan.Contract.Interface.Operations))
	for _, op := range plan.Contract.Interface.Operations {
		r, ok := routes[op.Name]
		if !ok {
			return "", fmt.Errorf("testgen: %s operation %s is no Manager's generated route", plan.Key, op.Name)
		}
		lines = append(lines, fmt.Sprintf("\t%q: {%q, %q},\n", toolName(op), r.Verb, r.Path))
	}
	sort.Strings(lines)
	lines = slices.Compact(lines)
	return "// clientRoutes is the generated route of each operation.\nvar clientRoutes = map[string]struct{ verb, path string }{\n" + strings.Join(lines, "") + "}\n\n", nil
}

// restHelpers is the in-process server the generated REST file runs. It names its types as the MCP
// dialect's, because the scenario bodies are the same code. scenariocompose hands it the generated
// handlers, built over the app's real authorization.
const restHelpers = `// toolRegistrar is the generated Handler of this client: it mounts its routes.
type toolRegistrar interface {
	Register(mux *http.ServeMux)
}

// toolSession is every scenario's subject: a client of the in-process server.
type toolSession struct {
	url string
	ctx context.Context
}

// serveTools mounts every handler on a fresh server and closes it when the test ends. Every request is
// served under ctx, the principal the session acts as; a ctx carrying none is authenticated as one
// scenario principal, as the server's middleware would.
func serveTools(ctx context.Context, t *testing.T, h *scenariohost.Host, handlers ...toolRegistrar) *toolSession {
	t.Helper()
	if _, ok := security.PrincipalFrom(ctx); !ok {
		ctx = security.WithPrincipal(ctx, security.Principal{Subject: "scenario"})
	}
	mux := http.NewServeMux()
	for _, r := range handlers {
		r.Register(mux)
	}
	server := httptest.NewUnstartedServer(mux)
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	server.Start()
	t.Cleanup(server.Close)
	return &toolSession{url: server.URL, ctx: ctx}
}

// Call requests the operation's route with args (a JSON object): a {param} in the path takes its
// value from args, and the rest is the query of a GET or the JSON body of anything else. A status
// of 400 or more is the call's error, carrying the response's text.
func (s *toolSession) Call(ctx context.Context, name, args string) (any, error) {
	route, ok := clientRoutes[name]
	if !ok {
		return nil, errors.New(name + ": no such route")
	}
	req, err := restRequest(ctx, route.verb, s.url+route.path, args)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
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
	for name, value := range fields {
		if hole := "{" + name + "}"; strings.Contains(target, hole) {
			target = strings.ReplaceAll(target, hole, url.PathEscape(fmt.Sprint(value)))
			delete(fields, name)
		}
	}
	var body io.Reader
	if verb == http.MethodGet {
		query := url.Values{}
		for name, value := range fields {
			query.Set(name, fmt.Sprint(value))
		}
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
	} else {
		raw, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	return http.NewRequestWithContext(ctx, verb, target, body)
}

`

// restErrorHasCode is the REST dialect's code match. A response carries the wire code of the
// Manager's error kind (not_found for NotFound), and a plan names the refusal as the contract's
// error model does, the same on both transports. The table mirrors httpgen's codeForKind; a
// scenario in archistrator's fresh-project tests holds the two together.
const restErrorHasCode = `// restWireCodes is the code a generated handler answers each Manager error kind with.
var restWireCodes = map[string]string{
	"ContractMisuse":     "contract_misuse",
	"NotFound":           "not_found",
	"Unauthorized":       "forbidden",
	"FailedPrecondition": "failed_precondition",
	"Infrastructure":     "infrastructure",
}

// errorHasCode matches the code, or the wire code of that error kind, in the error text.
func errorHasCode(err error, code string) bool {
	text := err.Error()
	wire, known := restWireCodes[code]
	return strings.Contains(text, code) || (known && strings.Contains(text, wire))
}

`
