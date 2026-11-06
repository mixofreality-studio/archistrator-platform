package composegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// Hook is one method of the generated Hooks interface, as the catalogue of seams the model's
// composition root has. The catalogue is DERIVED from the model; this is its read-only view for
// a generator that writes the hooks.go (the standard seed here, nullgen's conformance one).
type Hook struct {
	// Name is the method name, e.g. "ResolveProfile".
	Name string
	// Signature is the method's Go signature line, e.g. "ResolveProfile(cfg *Config) string".
	Signature string
	// Results are the method's result types, in order, as written in the signature.
	Results []string
	// Seed is the body the standard seed gives the method: a value that is always safe, or
	// "" where only the app can answer (the standard seed panics with FILL).
	Seed string
	// Finalizes is non-nil on a Finalize<Component> hook.
	Finalizes *Finalized
}

// Finalized describes the binding a Finalize<Component> hook is the post-construction seam of.
type Finalized struct {
	// Type is the constructed value's type as the hook spells it, e.g. "orderstate.OrderStateAccess".
	Type string
	// Null is the generated null implementation of Type (modelgen.GenerateNulls), e.g.
	// "orderstate.NullOrderStateAccess"; a struct{} value.
	Null string
}

// HookList is the Hooks interface's method set for the container cfg names, in interface order.
func HookList(m *projectmodel.Model, cfg Config) ([]Hook, error) {
	if err := validateConfig(m, cfg); err != nil {
		return nil, err
	}
	r, err := resolve(m, cfg)
	if err != nil {
		return nil, err
	}
	return hooksOf(r)
}

func hooksOf(r *resolved) ([]Hook, error) {
	derived := deriveHooks(r)
	out := make([]Hook, 0, len(derived))
	for _, h := range derived {
		results, err := resultTypes(h.line)
		if err != nil {
			return nil, err
		}
		name, _, _ := strings.Cut(h.line, "(")
		out = append(out, Hook{Name: name, Signature: h.line, Results: results, Seed: h.seed, Finalizes: h.finalizes})
	}
	return out, nil
}

// resultTypes parses a method signature line and returns its result types as written.
func resultTypes(line string) ([]string, error) {
	src := "package p\nfunc (appHooks) " + line + " {}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "hook.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("composegen: hook signature %q: %w", line, err)
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("composegen: hook signature %q is not a method", line)
	}
	var out []string
	if fn.Type.Results == nil {
		return out, nil
	}
	for _, field := range fn.Type.Results.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			out = append(out, types.ExprString(field.Type))
		}
	}
	return out, nil
}

// ExternalHTTPArgs is what the ExternalHTTP binding variant needs from the composition root
// (FIX-049): the args hook's types for each "<component>/ExternalHTTP" binding (the endpoint
// URL the deployment declares and the client the hook owns), and that its constructor returns
// an error. It is the one place both appgen and a hooks generator read it from.
func ExternalHTTPArgs(m *projectmodel.Model) (hooks map[string][]HookArgType, returnsError map[string]bool) {
	hooks, returnsError = map[string][]HookArgType{}, map[string]bool{}
	if m.Deployment == nil {
		return hooks, returnsError
	}
	for _, b := range m.Deployment.Bindings {
		for _, v := range b.PerProfile {
			if v.Variant != "ExternalHTTP" {
				continue
			}
			key := b.Component + "/" + v.Variant
			returnsError[key] = true
			hooks[key] = []HookArgType{{GoType: "string"}, {GoType: "*http.Client", GoImport: "net/http"}}
		}
	}
	return hooks, returnsError
}
