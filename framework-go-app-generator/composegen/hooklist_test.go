package composegen_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
)

// HookList is the Hooks interface's method set, in the order the seed writes it, with each
// method's result types and its Finalize<Component> binding.
func TestHookList_IsTheHooksInterface(t *testing.T) {
	m := loadGreenfield(t)
	hooks, err := composegen.HookList(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("HookList: %v", err)
	}
	byName := map[string]composegen.Hook{}
	for _, h := range hooks {
		byName[h.Name] = h
	}
	if got := byName["ResolveProfile"]; got.Seed != `return "local"` || !slices.Equal(got.Results, []string{"string"}) {
		t.Errorf("ResolveProfile = %+v", got)
	}
	if got := byName["TokenValidator"]; !slices.Equal(got.Results, []string{"security.Validator", "error"}) {
		t.Errorf("TokenValidator results = %v", got.Results)
	}
	if got, ok := byName["PolicyDecisionPoint"]; ok {
		t.Errorf("the authorization policy is generated, not a hook: %+v", got)
	}
	fin, ok := byName["FinalizeOrderStateAccess"]
	if !ok || fin.Finalizes == nil {
		t.Fatalf("no Finalize hook for orderStateAccess: %+v", hooks)
	}
	if fin.Finalizes.Type != "orderstate.Access" || fin.Finalizes.Null != "orderstate.NullAccess" {
		t.Errorf("Finalizes = %+v", fin.Finalizes)
	}
}

// HooksSeedWith replaces a body only where the option says so, and HooksSeed is the zero option.
func TestHooksSeedWith_ReplacesBodies(t *testing.T) {
	m := loadGreenfield(t)
	standard, err := composegen.HooksSeed(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("HooksSeed: %v", err)
	}
	same, err := composegen.HooksSeedWith(m, greenfieldCfg, composegen.SeedOptions{})
	if err != nil || !bytes.Equal(standard, same) {
		t.Fatalf("zero SeedOptions must equal HooksSeed (err=%v)", err)
	}
	got, err := composegen.HooksSeedWith(m, greenfieldCfg, composegen.SeedOptions{
		Header: "// header\n\n",
		Body: func(h composegen.Hook) string {
			if h.Name == "ResolveProfile" {
				return `return "replaced"`
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("HooksSeedWith: %v", err)
	}
	if !bytes.HasPrefix(got, []byte("// header\n\npackage main")) {
		t.Errorf("no header:\n%.80s", got)
	}
	if bytes.Contains(got, []byte(`return "local"`)) || !bytes.Contains(got, []byte(`return "replaced"`)) {
		t.Errorf("ResolveProfile body not replaced:\n%s", got)
	}
}

// ExternalHTTPArgs names the args hook and the error-returning constructor of each ExternalHTTP
// binding, and nothing for a model that has none.
func TestExternalHTTPArgs_NoneWithoutAnExternalBinding(t *testing.T) {
	m := loadGreenfield(t)
	hooks, returnsError := composegen.ExternalHTTPArgs(m)
	if len(hooks) != 0 || len(returnsError) != 0 {
		t.Errorf("greenfield binds no ExternalHTTP variant, got hooks=%v returnsError=%v", hooks, returnsError)
	}
}
