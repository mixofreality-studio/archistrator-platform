package clientgen

import (
	"fmt"
	"go/format"
	"path"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// wire.go generates a machine client's proof. A machine client authors nothing, so nothing is
// bound by hand and no plan exists, but what the actor depends on is still this project's own:
// its routes, its tool names, its serialization and how a refusal reaches the caller. Manager
// scenarios never cross that transport, and a generator test proves only the generator's
// fixtures. So each machine client's package gets a generated test that drives every route or
// tool it exposes to a Manager double and checks three things: the request reaches the right
// Manager operation, the actor sees the result, and the actor sees a refusal.
//
// A client with a mirror contract (methodcheck CLIENT-MACHINE-MIRROR) is left out: its plan
// already binds scenarios, and they own the same file name, the one the layout gate admits.

// wireFile is the one generated test file the arch layout gate admits in a Client package.
const wireFile = "client_scenarios.gen_test.go"

// wireImports are the imports of both proofs, gofmt-sorted by the caller's format.Source.
func wireImports(extra ...string) string {
	return strings.Join(append([]string{`"reflect"`, `"testing"`}, extra...), "\n\t")
}

// wireFake is the double: the Manager's generated fake with one operation answered by reflection,
// so no type of the contract has to be spelled again here.
const wireFake = `
// wireRefusal is what the refusing double returns.
var wireRefusal = fwmanager.New(fwmanager.NotFound, "wire refusal")

// wireDouble answers the one operation named by fn with its zero results and fail, and reports
// whether it was reached. Any other operation panics, because the request went to the wrong one.
func wireDouble(t *testing.T, fn string, fail error) (*fake.%[1]s, *bool) {
	t.Helper()
	double := &fake.%[1]s{}
	field := reflect.ValueOf(double).Elem().FieldByName(fn)
	if !field.IsValid() {
		t.Fatalf("the Manager double has no field %%s", fn)
	}
	reached := false
	field.Set(reflect.MakeFunc(field.Type(), func([]reflect.Value) []reflect.Value {
		reached = true
		out := make([]reflect.Value, field.Type().NumOut())
		for i := range out {
			out[i] = reflect.Zero(field.Type().Out(i))
		}
		if last := len(out) - 1; last >= 0 && fail != nil {
			out[last] = reflect.ValueOf(fail)
		}
		return out
	}))
	return double, &reached
}
`

// restWire is the proof of one Manager's REST handlers, for a REST machine client.
func (g *contractGen) restWire(files map[string][]byte) error {
	plans, err := httpgen.PlanOps(g.doc)
	if err != nil {
		return fmt.Errorf("plan the routes: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, wireHeader, g.pkg, wireImports(`"context"`, `"net/http"`, `"net/http/httptest"`, `"strings"`), g.imports(path.Join(g.module, webRoot, g.pkg), `fwsecurity "`+securityImportPath+`"`))
	b.WriteString(wireFake)
	b.WriteString(restWireHelpers)
	for _, p := range plans {
		want := "http.StatusNoContent"
		if p.HasResult {
			want = "http.StatusOK"
		}
		fmt.Fprintf(&b, `
func TestWire%[1]s(t *testing.T) {
	const method, target = %[2]q, %[3]q
	double, reached := wireDouble(t, %[4]q, nil)
	if got := restCall(double, method, target); got != %[5]s || !*reached {
		t.Fatalf("%%s %%s: status %%d, reached %%v; the route must reach %[1]s and answer with its result", method, target, got, *reached)
	}
	double, reached = wireDouble(t, %[4]q, wireRefusal)
	if got := restCall(double, method, target); got != http.StatusNotFound || !*reached {
		t.Fatalf("%%s %%s: status %%d, reached %%v; the caller must see %[1]s's refusal", method, target, got, *reached)
	}
}
`, p.Name, p.Verb, restTarget(p), p.Name+"Fn", want)
	}
	return g.writeWire(files, path.Join(webRoot, g.pkg, wireFile), b.String())
}

// restTarget is a route with a value of the right kind in each path and query parameter.
func restTarget(p httpgen.OpPlan) string {
	target := p.PathTemplate
	for _, param := range p.PathParams {
		target = strings.ReplaceAll(target, "{"+param.Name+"}", restSample(param.ScalarKind))
	}
	var query []string
	for _, param := range p.QueryParams {
		query = append(query, param.Name+"="+restSample(param.ScalarKind))
	}
	if len(query) > 0 {
		target += "?" + strings.Join(query, "&")
	}
	return target
}

func restSample(kind string) string {
	switch kind {
	case "uuid":
		return "00000000-0000-0000-0000-000000000001"
	case "integer", "number":
		return "1"
	case "boolean":
		return "true"
	default:
		return "x"
	}
}

const restWireHelpers = `
// permitAll answers every authorization: the proof is of the transport, and the app's own
// policy is a composition choice its hooks make.
type permitAll struct{ fwsecurity.Security }

func (permitAll) Authorize(context.Context, fwsecurity.Principal, fwsecurity.Action, fwsecurity.ResourceRef) (fwsecurity.Decision, error) {
	return fwsecurity.Decision{Permit: true}, nil
}

// restCall drives one request through the mounted routes and returns its status.
func restCall(double *fake.%[1]s, method, target string) int {
	mux := http.NewServeMux()
	(&client.Handler{Manager: double, Security: permitAll{}}).Register(mux)
	var body *strings.Reader = strings.NewReader("")
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	req := httptest.NewRequest(method, target, body)
	req = req.WithContext(fwsecurity.WithPrincipal(req.Context(), fwsecurity.Principal{Subject: "wire"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}
`

// mcpWire is the proof of one Manager's tool table, for an MCP machine client: the tools are
// listed and called over an in-memory transport, each with arguments made from the input schema
// the server itself advertises, so the schema and the handler are held to each other.
func (g *contractGen) mcpWire(files map[string][]byte, c projectmodel.SystemComponent) error {
	dir := path.Join(ClientPackage(c.Name), g.pkg)
	var b strings.Builder
	fmt.Fprintf(&b, wireHeader, g.pkg, wireImports(`"context"`, `"encoding/json"`, `"time"`), g.imports(path.Join(g.module, dir), `"github.com/modelcontextprotocol/go-sdk/mcp"`))
	b.WriteString(wireFake)
	b.WriteString(mcpWireHelpers)
	prefix := projectmodel.LowerFirst(g.doc.ManagerBase())
	for _, op := range g.doc.Interface.Operations {
		fmt.Fprintf(&b, `
func TestWire%[1]s(t *testing.T) {
	const tool = %[2]q
	double, reached := wireDouble(t, %[3]q, nil)
	res, err := mcpCall(t, double, tool)
	if err != nil || res.IsError || !*reached {
		t.Fatalf("%%s: %%v, %%+v, reached %%v; the tool must reach %[1]s and answer with its result", tool, err, res, *reached)
	}
	if %[4]v && res.StructuredContent == nil {
		t.Fatalf("%%s: the caller must see %[1]s's result", tool)
	}
	double, reached = wireDouble(t, %[3]q, wireRefusal)
	res, err = mcpCall(t, double, tool)
	if !*reached || (err == nil && !res.IsError) {
		t.Fatalf("%%s: %%v, %%+v, reached %%v; the caller must see %[1]s's refusal", tool, err, res, *reached)
	}
}
`, op.Name, prefix+op.Name, op.Name+"Fn", op.Result != nil)
	}
	return g.writeWire(files, path.Join(dir, wireFile), b.String())
}

// #nosec G101 -- generated test source, and the sample uuid is no credential.
const mcpWireHelpers = `
// mcpCall lists the tools of a server over the double and calls one, with arguments its own
// advertised input schema admits.
func mcpCall(t *testing.T, double *fake.%[1]s, tool string) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "wire", Version: "0"}, nil)
	(&client.Handler{Manager: double}).Register(srv)
	serverEnd, clientEnd := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverEnd, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "wire", Version: "0"}, nil).Connect(ctx, clientEnd, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range listed.Tools {
		if candidate.Name != tool {
			continue
		}
		raw, err := json.Marshal(candidate.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		defs, _ := schema["$defs"].(map[string]any)
		return session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: wireSample(schema, defs)})
	}
	t.Fatalf("the server lists no tool %%q", tool)
	return nil, nil
}

// wireSample is a value a JSON schema admits: its required members only, the first of an enum
// or a union, and a string that also parses as a time.
func wireSample(schema, defs map[string]any) any {
	if ref, ok := schema["$ref"].(string); ok {
		target, _ := defs[ref[len("#/$defs/"):]].(map[string]any)
		return wireSample(target, defs)
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	for _, union := range []string{"oneOf", "anyOf"} {
		if branches, ok := schema[union].([]any); ok && len(branches) > 0 {
			first, _ := branches[0].(map[string]any)
			return wireSample(first, defs)
		}
	}
	switch wireType(schema) {
	case "object":
		out := map[string]any{}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, name := range required {
			key, _ := name.(string)
			property, _ := properties[key].(map[string]any)
			out[key] = wireSample(property, defs)
		}
		return out
	case "array":
		return []any{}
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "string":
		if schema["format"] == "uuid" {
			return "00000000-0000-0000-0000-000000000001"
		}
		return "2026-01-02T03:04:05Z"
	default:
		return map[string]any{}
	}
}

// wireType is a schema's JSON type, its first non-null one when it is a union.
func wireType(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		for _, one := range t {
			if name, ok := one.(string); ok && name != "null" {
				return name
			}
		}
	}
	return ""
}
`

const wireHeader = `// Code generated by clientgen. DO NOT EDIT.

package %[1]s_test

import (
	%[2]s

	%[3]s
)
`

const (
	frameworkManagerImportPath = "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	securityImportPath         = "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// imports is the proof's module imports: the client package under test, the Manager's fake and
// the framework's error kinds.
func (g *contractGen) imports(clientImport string, extra string) string {
	lines := []string{
		`client "` + clientImport + `"`,
		`fake "` + g.managerImport + `/fake"`,
		`fwmanager "` + frameworkManagerImportPath + `"`,
		extra,
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n\t")
}

// writeWire formats one proof file: the helper constants name the fake type as %[1]s and keep
// their own format verbs as %%.
func (g *contractGen) writeWire(files map[string][]byte, rel, src string) error {
	src = strings.ReplaceAll(src, "%[1]s", "Fake"+g.doc.Interface.Name)
	src = strings.ReplaceAll(src, "%%", "%")
	out, err := format.Source([]byte(src))
	if err != nil {
		return fmt.Errorf("format %s: %w\n%s", rel, err, src)
	}
	files[rel] = out
	return nil
}
