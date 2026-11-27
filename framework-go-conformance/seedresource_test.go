package conformance

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// One seedResource fixture type end to end, with no container: a fixture TP-ARRANGE admits on a plan
// binding is emitted by testgen and accepted by the scenario host's decode and dispatch; a fixture
// that is the domain's own shape (the RUN-032 failure) is refused by TP-ARRANGE, before a run.

const (
	seedFixture  = "todomvc"
	seedPlan     = "listManager"
	seedScenario = "add-a-todo-P1"
	seedResource = "TodoListStore" // the fixture's Resource component
)

// withSeed is the fixture's project.json with the arrange of seedPlan's seedScenario replaced by one
// seedResource of the given fixture.
func withSeed(t *testing.T, raw []byte, fixture string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	plans, _ := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)
	plan, _ := plans[seedPlan].(map[string]any)
	bindings, _ := plan["bindings"].([]any)
	for _, b := range bindings {
		bm, _ := b.(map[string]any)
		if bm["scenario"] != seedScenario {
			continue
		}
		bm["arrange"] = []any{map[string]any{"kind": "seedResource", "model": map[string]any{
			"resource": seedResource, "fixture": json.RawMessage(fixture),
		}}}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("the fixture's %s plan has no %s binding", seedPlan, seedScenario)
	return nil
}

// arrangeFindings are the TP-ARRANGE errors the design check reports for raw.
func arrangeFindings(t *testing.T, raw []byte) []methodcheck.Finding {
	t.Helper()
	all, err := methodcheck.ValidateProjectJSON(raw, methodcheck.ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()})
	if err != nil {
		t.Fatalf("ValidateProjectJSON: %v", err)
	}
	var out []methodcheck.Finding
	for _, f := range all {
		if f.RuleID == "TP-ARRANGE" {
			out = append(out, f)
		}
	}
	return out
}

// hostSeed posts fixture to a bare scenario host's /seedResource and returns the status and text. The
// host booted nothing, so a fixture its decode and dispatch accept stops at "the host has no <substrate>";
// one they refuse stops at "resource seed".
func hostSeed(t *testing.T, fixture string) (int, string) {
	t.Helper()
	srv, err := (&scenariohost.Host{}).ServeArrange(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	body, err := json.Marshal(map[string]any{"resource": seedResource, "fixture": json.RawMessage(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/seedResource", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	text, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(text)
}

func TestSeedResource_EveryAdmittedVariantReachesTheHost(t *testing.T) {
	raw, err := Fixture(seedFixture)
	if err != nil {
		t.Fatal(err)
	}
	admitted := map[string]struct{ fixture, substrate string }{
		"postgres": {`{"kind":"postgres","model":{"sql":"insert into todos (id) values ('t1')"}}`, "no Postgres"},
		"github":   {`{"kind":"github","model":{"owner":"acme","name":"widget","description":"d","topics":["t"],"private":true,"files":{"README.md":"# hi"}}}`, "no GitHub"},
		"gitRepo":  {`{"kind":"gitRepo","model":{"files":{"docs/a.txt":"x"}}}`, "no local repo"},
	}
	for name, c := range admitted {
		t.Run(name, func(t *testing.T) {
			planned := withSeed(t, raw, c.fixture)
			if fs := arrangeFindings(t, planned); len(fs) != 0 {
				t.Fatalf("TP-ARRANGE refused an admitted %s seed: %v", name, fs)
			}
			out, err := testgen.Generate(planned, testgen.Config{ModulePath: "example.com/conformance/" + seedFixture})
			if err != nil {
				t.Fatalf("testgen.Generate: %v", err)
			}
			emitted := false
			for _, src := range out.Generated {
				if strings.Contains(string(src), `h.SeedResource(t, "`+seedResource+`", json.RawMessage(`) && strings.Contains(string(src), `"kind":"`+name+`"`) {
					emitted = true
				}
			}
			if !emitted {
				t.Fatalf("testgen emitted no h.SeedResource call for the %s seed", name)
			}
			code, text := hostSeed(t, c.fixture)
			if code != http.StatusInternalServerError || !strings.Contains(text, c.substrate) {
				t.Fatalf("the host did not accept the %s seed: %d %q, want it to reach the substrate (%q)", name, code, text, c.substrate)
			}
		})
	}
}

func TestSeedResource_DomainShapedFixtureIsRefusedBeforeARun(t *testing.T) {
	raw, err := Fixture(seedFixture)
	if err != nil {
		t.Fatal(err)
	}
	domain := `{"owner":"alice","todos":[{"title":"Buy milk"}]}`
	fs := arrangeFindings(t, withSeed(t, raw, domain))
	if len(fs) == 0 {
		t.Fatal("TP-ARRANGE admitted a domain-shaped fixture")
	}
	if msg := fs[0].Message; !strings.Contains(msg, seedResource) || !strings.Contains(msg, `"kind":"postgres"`) {
		t.Errorf("the finding must name the resource and the seed shapes: %s", msg)
	}
	if code, text := hostSeed(t, domain); code != http.StatusInternalServerError || !strings.Contains(text, "resource seed") {
		t.Errorf("the host must refuse it too: %d %q", code, text)
	}
}
