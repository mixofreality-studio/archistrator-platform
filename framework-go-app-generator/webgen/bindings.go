package webgen

import (
	"fmt"
	"sort"
	"strings"
)

// Binding is one OpsClient op: how REST reaches it and which MCP tool answers it.
type Binding struct {
	OpID   string
	Method string // upper-case HTTP verb
	Path   string // the OAS path template
	// Tool is the MCP tool the server registers for the op; "" binds `tool: null`,
	// which the MCP transport refuses loudly.
	Tool string
	// Composition marks a route mounted by the Go composition root rather than a
	// manager contract. It is REST-only and its REST call sends
	// `Accept: application/json`.
	Composition bool
}

// CompositionRoute is a hand-declared route beside the OAS: mounted by the Go
// composition root (framework-go's /api/userinfo, an app's ExtraMounts), so the
// OAS does not carry it and no MCP tool exists for it. Binding it closes a raw
// fetch that would otherwise bypass the OpsClient seam, and lets the fixture
// schema type its fixtures.
type CompositionRoute struct {
	OpID   string
	Method string
	Path   string
	// Result is the JSON Schema of the route's 200 body, in the key order the
	// config wrote it.
	Result Value
}

// httpMethods is openapi-fetch's verb set; the OAS uses GET and POST today.
var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// DeriveBindings derives the binding of every operation in the OAS. Every path
// has the httpgen shape /api/v1/<mgr>/<op>[/{param}...]; the OpId is
// camel(mgr)+Pascal(op), and the tool is camel(mgr)+operationId when the server
// registers it (tools), which is how mcpemit and mcpgen name tools. A path of
// any other shape, a missing operationId or a colliding OpId fails loudly.
func DeriveBindings(doc Value, tools map[string]bool) ([]Binding, error) {
	paths := obj(path(doc, "paths"))
	if paths == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []Binding
	for _, p := range paths.Keys() {
		methods, _ := paths.Get(p)
		bs, err := pathBindings(p, obj(methods), tools, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, bs...)
	}
	return sortBindings(out)
}

func pathBindings(p string, methods *Object, tools map[string]bool, seen map[string]bool) ([]Binding, error) {
	segs := nonEmpty(strings.Split(p, "/"))
	if len(segs) < 4 || segs[0] != "api" || segs[1] != "v1" {
		return nil, fmt.Errorf("webgen: path does not match the /api/v1/<mgr>/<op>[/{...}] shape: %s", p)
	}
	mgr, op := segs[2], segs[3]
	opID := camelCase(mgr) + pascalCase(op)
	var out []Binding
	for _, m := range httpMethods {
		operation, ok := methods.Get(m)
		if !ok {
			continue
		}
		b, err := methodBinding(p, strings.ToUpper(m), mgr, opID, operation, tools, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// methodBinding binds one method of one path; seen catches OpId collisions.
func methodBinding(p, verb, mgr, opID string, operation Value, tools, seen map[string]bool) (Binding, error) {
	if seen[opID] {
		return Binding{}, fmt.Errorf("webgen: duplicate derived opId %q (from %s %s) — mgr/op derivation collided with an earlier binding", opID, p, verb)
	}
	seen[opID] = true
	operationID, _ := path(operation, "operationId").(string)
	if operationID == "" {
		return Binding{}, fmt.Errorf("webgen: %s %s has no operationId", verb, p)
	}
	b := Binding{OpID: opID, Method: verb, Path: p}
	if tool := camelCase(mgr) + operationID; tools[tool] {
		b.Tool = tool
	}
	return b, nil
}

// WithComposition binds the composition routes beside the OAS bindings. A route
// whose OpId collides with a derived one fails, as a derived collision does.
func WithComposition(bindings []Binding, routes []CompositionRoute) ([]Binding, error) {
	have := map[string]bool{}
	for _, b := range bindings {
		have[b.OpID] = true
	}
	out := append([]Binding(nil), bindings...)
	for _, r := range routes {
		if have[r.OpID] {
			return nil, fmt.Errorf("webgen: composition route %q collides with a derived OAS binding", r.OpID)
		}
		have[r.OpID] = true
		out = append(out, Binding{OpID: r.OpID, Method: r.Method, Path: r.Path, Composition: true})
	}
	return sortBindings(out)
}

// sortBindings orders by OpId the way the reference generator's
// String.prototype.localeCompare does. For [A-Za-z0-9] that is the ICU root
// collation: case-insensitive first, then lower case before upper case at the
// first case difference. Any other character would make the order depend on
// collation details this port does not model, so it is refused.
func sortBindings(bs []Binding) ([]Binding, error) {
	for _, b := range bs {
		if !isAlnum(b.OpID) {
			return nil, fmt.Errorf("webgen: opId %q is outside [A-Za-z0-9]", b.OpID)
		}
	}
	sort.SliceStable(bs, func(i, j int) bool { return collate(bs[i].OpID, bs[j].OpID) < 0 })
	return bs, nil
}

func collate(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	for i := 0; i < len(a); i++ {
		la, lb := isLower(a[i]), isLower(b[i])
		if la != lb {
			if la {
				return -1
			}
			return 1
		}
	}
	return 0
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func isAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isAlnumByte(s[i]) {
			return false
		}
	}
	return s != ""
}

func isAlnumByte(c byte) bool {
	return isLower(c) || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func pascalCase(kebab string) string {
	var b strings.Builder
	for _, w := range nonEmpty(strings.Split(kebab, "-")) {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

func camelCase(kebab string) string {
	p := pascalCase(kebab)
	if p == "" {
		return p
	}
	return strings.ToLower(p[:1]) + p[1:]
}
