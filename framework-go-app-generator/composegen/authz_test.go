package composegen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
)

// TestAuthzTableNamesEveryGeneratedRequest (FIX-046): the composition root carries one row per
// operation each web-exposed Manager's generated transports serve, read from httpgen.PlanOps,
// and the framework's own test that holds the policy to them. RUN-015's hand-written policy
// refused requests the generated handlers really sent, and no gate in the activity that wrote
// it ever asked; the SPA found it as a 403 on every per-todo call.
func TestAuthzTableNamesEveryGeneratedRequest(t *testing.T) {
	m := loadGreenfield(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	table := string(got[composegen.AuthzTableFile])
	want := 0
	for _, key := range []string{"fulfillmentManager", "orderManager"} {
		plans, err := httpgen.PlanOps(m.Contracts[key].Doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range plans {
			row := fmt.Sprintf("{Manager: %q, Op: %q, Verb: %q, Scope: %q},", m.Contracts[key].Doc.Interface.Name, p.Name, p.AuthzVerb, p.AuthzScope)
			if !strings.Contains(table, row) {
				t.Errorf("the table lacks %s:\n%s", row, table)
			}
			want++
		}
	}
	if n := strings.Count(table, "{Manager: "); n != want || want == 0 {
		t.Errorf("the table has %d rows, want one per served operation (%d)", n, want)
	}

	test := string(got[composegen.AuthzTestFile])
	for _, w := range []string{
		"func TestPolicyAdmitsEveryGeneratedRequest(t *testing.T) {",
		"var hooks Hooks = new(appHooks)",
		"principal := hooks.DevConfig(cfg).Principal",
		"sec := edgeSecurity(hooks)",
		"security.ResourceRef{Kind: row.Scope, ID: principal.Subject})",
	} {
		if !strings.Contains(test, w) {
			t.Errorf("the generated policy test lacks %q:\n%s", w, test)
		}
	}
	main := string(got[composegen.MainFile])
	for _, w := range []string{"sec := edgeSecurity(hooks)", "hooks.ExtraMounts(root, cfg, dev, validator, sec, webManagers)"} {
		if !strings.Contains(main, w) {
			t.Errorf("main.gen.go lacks %q: the server must serve with the policy the test holds, and hand MCP mounts the same sec", w)
		}
	}

	// Narrowed like clientgen's Exposed, the table follows what the transports really serve.
	plans, err := httpgen.PlanOps(m.Contracts["orderManager"].Doc)
	if err != nil {
		t.Fatal(err)
	}
	narrowed := greenfieldCfg
	narrowed.ExposedOps = map[string][]string{"orderManager": {plans[0].Name}, "fulfillmentManager": nil}
	got, err = composegen.Generate(m, narrowed)
	if err != nil {
		t.Fatal(err)
	}
	table = string(got[composegen.AuthzTableFile])
	if n := strings.Count(table, `{Manager: "OrderManager"`); n != 1 {
		t.Errorf("a narrowed Manager keeps only its served operations, got %d OrderManager rows:\n%s", n, table)
	}

	// A container with no web-exposed Manager serves no request and gets neither file.
	none := greenfieldCfg
	none.WebExposedManagers = []string{}
	got, err = composegen.Generate(m, none)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[composegen.AuthzTableFile]; ok {
		t.Error("a worker-only container got an authorization table")
	}
	if _, ok := got[composegen.AuthzTestFile]; ok {
		t.Error("a worker-only container got a policy test")
	}
}
