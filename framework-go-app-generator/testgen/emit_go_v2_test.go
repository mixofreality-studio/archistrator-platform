package testgen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// generateMutated emits the shared fixture after mutate has edited its decoded document.
func generateMutated(t *testing.T, mutate func(doc map[string]any)) Output {
	t.Helper()
	doc := fixtureDoc(t)
	mutate(doc)
	out, err := generateDoc(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// planBinding is the binding of scenario id in component's test plan.
func planBinding(t *testing.T, doc map[string]any, component, id string) map[string]any {
	t.Helper()
	rec := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)[component].(map[string]any)
	for _, b := range rec["bindings"].([]any) {
		if b.(map[string]any)["scenario"] == id {
			return b.(map[string]any)
		}
	}
	t.Fatalf("no binding %s/%s", component, id)
	return nil
}

func TestGo_NoHooksFileEmitted(t *testing.T) {
	out := generate(t)
	for p := range out.Generated {
		if strings.HasSuffix(p, "_hooks_test.go") {
			t.Errorf("a Go hooks file was emitted: %s", p)
		}
	}
	src := string(out.Generated[billingGen])
	for _, want := range []string{
		// the subject and the replay registration come from the generated scenariocompose package
		"var subject billing.BillingManager = scenariocompose.NewBillingManager(t, h)",
		"\"example.com/app/server/internal/scenariocompose\"",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	for _, bad := range []string{"newSubject", "replayWorkflows", "callContext", "probeProcessOrder", "FILL"} {
		if strings.Contains(src, bad) {
			t.Errorf("the generated file must not mention %q:\n%s", bad, src)
		}
	}
}

// The compose package's path is the one composegen emits to.
func TestGo_ComposeImportIsComposegens(t *testing.T) {
	if want := path.Dir(composegen.ScenarioComposeFile); scenarioComposeDir != want {
		t.Fatalf("scenarioComposeDir = %q, want %q", scenarioComposeDir, want)
	}
}

func TestGo_PrincipalFromPlan(t *testing.T) {
	raw, err := os.ReadFile("testdata/project_tp.json")
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.ReplaceAll(string(raw), `"customer"`, `"team-member"`)
	out, err := Generate([]byte(renamed), Config{ModulePath: "example.com/app/server"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(out.Generated[billingGen])
	for _, want := range []string{
		"h.ManagerContextAs(t, principal(\"team-member\"))",
		"var principals = map[string]scenariohost.Principal{",
		"\"team-member\": {Subject: \"user-customer\", Roles: []string{\"team-member\"}},",
		"func principal(name string) scenariohost.Principal {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
}

// A step whose actor has no principal runs on the host's anonymous context, never a made-up identity.
func TestGo_NoPrincipalRunsAnonymously(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) {
		plans := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)
		for _, rec := range plans {
			rec.(map[string]any)["principals"] = map[string]any{}
		}
	})
	src := string(out.Generated[billingGen])
	if !strings.Contains(src, "h.ManagerContext(t)") || strings.Contains(src, "principal(") {
		t.Errorf("a principal-less plan runs on the anonymous context:\n%s", src)
	}
}

// refStepsFixture gives orderManager's track-order scenario a second stimulus, so a later step can read an
// earlier one's result.
func refStepsFixture(doc map[string]any) {
	design := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	for _, v := range design["dynamicViews"].([]any) {
		view := v.(map[string]any)
		if view["useCaseId"] != "track-order" {
			continue
		}
		step := view["steps"].([]any)[0].(map[string]any)
		step["calls"] = append(step["calls"].([]any), map[string]any{"from": "customer", "to": "order-manager", "mode": "sync", "label": "PlaceOrder", "alt": nil})
	}
}

// withRefSteps binds orderManager's track-order scenario to two steps: step 2 reads step 1's whole result as
// its argument, and a probe of step 2 reads a path of step 1's result.
func withRefSteps(t *testing.T, doc map[string]any) {
	t.Helper()
	refStepsFixture(doc)
	b := planBinding(t, doc, "orderManager", "track-order-P1")
	b["steps"] = []any{
		map[string]any{"seq": 1, "operation": "GetOrder",
			"inputs": []any{map[string]any{"name": "orderId", "value": lit("o-2")}},
			"expect": map[string]any{"result": `{"id":"o-2"}`, "errorExpected": false}, "probes": []any{}},
		map[string]any{"seq": 2, "operation": "PlaceOrder",
			"inputs": []any{map[string]any{"name": "order", "value": map[string]any{"kind": "ref", "model": map[string]any{"step": 1, "path": ""}}}},
			"expect": map[string]any{"result": "o-2", "errorExpected": false},
			"probes": []any{map[string]any{"for": "Order[open]", "component": "billingManager", "operation": "GetInvoice",
				"inputs": []any{map[string]any{"name": "invoiceId", "value": map[string]any{"kind": "ref", "model": map[string]any{"step": 1, "path": "id"}}}},
				"expect": map[string]any{"result": `{"id":"inv-o-2"}`, "errorExpected": false}}}},
	}
}

func TestGo_RefThreading(t *testing.T) {
	out := generateMutated(t, func(doc map[string]any) { withRefSteps(t, doc) })
	src := string(out.Generated[orderGen])
	for _, want := range []string{
		// step 2's order argument is step 1's whole result, threaded in before the call
		"refInto(t, \"track-order-P1\", 2, out1, \"\", &in2.Order)",
		// a probe's argument reads a path of step 1's result
		"\"id\", &in.InvoiceId)",
		// the cross-component probe is a direct call on the other component, as the step's principal
		"scenariocompose.NewBillingManager(t, h).GetInvoice(h.ManagerContextAs(t, principal(\"customer\")), in.InvoiceId)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	if strings.Contains(src, "ref arguments are not resolved") {
		t.Errorf("a ref must be threaded, not refused:\n%s", src)
	}
}

// A ref to a result that is not there is a generation error, never a silently zero argument.
func TestGo_RefInArrangeIsRefused(t *testing.T) {
	doc := fixtureDoc(t)
	b := planBinding(t, doc, "billingManager", "process-order-P2")
	b["arrange"] = []any{map[string]any{"kind": "contractCall", "model": map[string]any{
		"component": "orderManager", "operation": "GetOrder",
		"inputs": []any{map[string]any{"name": "orderId", "value": map[string]any{"kind": "ref", "model": map[string]any{"step": 1, "path": "id"}}}},
		"expect": map[string]any{"errorExpected": false}}}}
	if _, err := generateDoc(t, doc); err == nil || !strings.Contains(err.Error(), "arrange") {
		t.Fatalf("an arrange argument cannot read a step result that has not run: %v", err)
	}
}

func arrangeAllKinds(doc map[string]any) {
	b := planBindingNoT(doc, "billingManager", "process-order-P2")
	b["arrange"] = []any{
		map[string]any{"kind": "contractCall", "model": map[string]any{
			"component": "orderManager", "operation": "GetOrder",
			"inputs": []any{map[string]any{"name": "orderId", "value": lit("o-2")}},
			"expect": map[string]any{"result": `{"id":"o-2"}`, "errorExpected": false}}},
		map[string]any{"kind": "advanceClock", "model": map[string]any{"duration": "1h"}},
		map[string]any{"kind": "failNext", "model": map[string]any{"component": "orderManager", "operation": "PlaceOrder", "errorCode": "Down"}},
		map[string]any{"kind": "seedResource", "model": map[string]any{"resource": "postgres", "fixture": map[string]any{"sql": "insert into t values (1)"}}},
	}
}

func planBindingNoT(doc map[string]any, component, id string) map[string]any {
	rec := doc["phaseArtifacts"].(map[string]any)["testPlan"].(map[string]any)[component].(map[string]any)
	for _, b := range rec["bindings"].([]any) {
		if b.(map[string]any)["scenario"] == id {
			return b.(map[string]any)
		}
	}
	return nil
}

func TestGo_ArrangeExhaustive(t *testing.T) {
	out := generateMutated(t, arrangeAllKinds)
	src := string(out.Generated[billingGen])
	want := []string{
		"scenariocompose.NewOrderManager(t, h).GetOrder(h.ManagerContext(t), in.OrderId)",
		"expect(t, \"process-order-P2 arrange 1\", 0, aout, aerr,",
		"h.AdvanceClock(t, time.Duration(3600000000000))",
		"h.FailNext(t, \"orderManager\", \"PlaceOrder\", \"Down\")",
		"h.SeedResource(t, \"postgres\", json.RawMessage(`{\"sql\":\"insert into t values (1)\"}`))",
		"\"time\"",
	}
	last := -1
	for _, w := range want[:5] {
		i := strings.Index(src, w)
		if i < 0 {
			t.Fatalf("missing %q in\n%s", w, src)
		}
		if w == want[0] || w == want[2] || w == want[3] || w == want[4] {
			if i < last {
				t.Errorf("arrange lines must keep the plan's order; %q came early", w)
			}
			last = i
		}
	}
	if !strings.Contains(src, want[5]) {
		t.Errorf("an advanceClock needs the time import:\n%s", src)
	}
	// arrange runs before the first step
	if strings.Index(src, "h.SeedResource") > strings.Index(src, "// step 1: RecordPayment") {
		t.Errorf("arrange must precede step 1:\n%s", src)
	}
}

// The arrange emitter's switch over the sealed ArrangeCall has one case per variant and no default, so
// gochecksumtype (and a missing case at review) catches a fifth kind nobody emits.
func TestGo_ArrangeSwitchIsExhaustiveWithoutDefault(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "emit_arrange.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var cases []string
	hasDefault := false
	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		for _, c := range sw.Body.List {
			cc := c.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			for _, e := range cc.List {
				if sel, ok := e.(*ast.SelectorExpr); ok {
					cases = append(cases, sel.Sel.Name)
				}
			}
		}
		return true
	})
	if hasDefault {
		t.Error("the arrange switch has a default arm")
	}
	for _, kind := range []string{"ContractCall", "AdvanceClock", "FailNext", "SeedResource"} {
		found := false
		for _, c := range cases {
			found = found || c == kind
		}
		if !found {
			t.Errorf("the arrange switch lacks a case for %s (has %v)", kind, cases)
		}
	}
}

// ---- the generated differential, compiled and run ----

const stubBilling = `package billing

import fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

type Invoice struct {
	ID    string ` + "`json:\"id\"`" + `
	State string ` + "`json:\"state\"`" + `
}

type BillingManager interface {
	RecordPayment(ctx fwm.Context, invoiceId string, amount int64) error
	GetInvoice(ctx fwm.Context, invoiceId string) (Invoice, error)
}
`

// stubCompose builds the subject: a null one answers "paid" whatever happened, a real one is paid only
// after RecordPayment.
const stubCompose = `package scenariocompose

import (
	"os"
	"testing"

	"example.com/app/server/internal/manager/billing"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

type subject struct{ paid, null bool }

func (s *subject) RecordPayment(fwm.Context, string, int64) error { s.paid = true; return nil }
func (s *subject) GetInvoice(fwm.Context, string) (billing.Invoice, error) {
	if s.paid || s.null {
		return billing.Invoice{ID: "inv-o-2", State: "paid"}, nil
	}
	return billing.Invoice{ID: "inv-o-2", State: "requested"}, nil
}

func NewBillingManager(t *testing.T, h *scenariohost.Host) billing.BillingManager {
	return &subject{null: os.Getenv("STUB_SUBJECT") == "null"}
}

func NullBillingManager(inner billing.BillingManager, ops ...string) billing.BillingManager { return inner }

func Workflows() scenariohost.Workflows { return scenariohost.Workflows{} }
`

// runGenerated builds a module around the generated billing scenarios file and runs one scenario with the
// subject the env asks for, returning go test's combined output.
func runGenerated(t *testing.T, gen, nullFile []byte, run, subject string) (string, error) {
	t.Helper()
	root := writeModule(t, map[string]string{
		"internal/manager/billing/billing.go":                    stubBilling,
		"internal/scenariocompose/compose.go":                    stubCompose,
		"internal/manager/billing/manager_scenarios.gen_test.go": string(gen),
		billingNull: string(nullFile),
	}, moduleOpts{})
	return goIn(root, []string{"STUB_SUBJECT=" + subject}, "test", "-count=1", "-run", run, "./internal/manager/billing/")
}

// TestGo_DifferentialFailsVacuous compiles the generated file and runs its process-order scenario against a
// null subject, whose invoice reads "paid" before and after: the probe's expectation held before the step
// ran, so the scenario proves nothing and must fail as vacuous. Against a subject that really pays, the same
// file passes.
func TestGo_DifferentialFailsVacuous(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	generated := generate(t).Generated
	gen, nullFile := generated[billingGen], generated[billingNull]
	out, err := runGenerated(t, gen, nullFile, "^TestScenario_process_order_P2$", "null")
	if err == nil {
		t.Fatalf("a null subject passed the scenario:\n%s", out)
	}
	if !strings.Contains(out, "vacuous") {
		t.Fatalf("the failure must say vacuous:\n%s", out)
	}
	out, err = runGenerated(t, gen, nullFile, "^TestScenario_process_order_P2$", "real")
	if err != nil {
		t.Fatalf("a subject that really pays must pass the scenario: %v\n%s", err, out)
	}
}

func TestMCP_NoPermitAll(t *testing.T) {
	out := generateShopTools(t)
	src := string(out.Generated[toolsGen])
	for _, bad := range []string{"permitAll", "newSubject", "probeTrackOrderP1S1N2", "Permit: true"} {
		if strings.Contains(src, bad) {
			t.Errorf("an MCP client file must not carry %q:\n%s", bad, src)
		}
	}
	for _, want := range []string{
		"scenariocompose.NewShopTools(t, h)",
		"serveTools(h.PrincipalContext(t, principal(\"customer\")), t, h, handlers)",
		// the cross-component probe is a direct call, not a hook
		"scenariocompose.NewOrderManager(t, h).GetOrder(h.ManagerContextAs(t, principal(\"customer\")), in.OrderId)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
	if _, ok := out.Generated[toolsHooks]; ok {
		t.Error("an MCP client gets no hooks file")
	}
}

// ---- the generated files, type-checked ----

const stubOrder = `package order

import fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

type Order struct {
	ID string ` + "`json:\"id\"`" + `
}

type OrderID string

type OrderManager interface {
	PlaceOrder(ctx fwm.Context, order Order) (OrderID, error)
	GetOrder(ctx fwm.Context, orderId OrderID) (Order, error)
}
`

const stubComposeBoth = `package scenariocompose

import (
	"testing"

	"example.com/app/server/internal/manager/billing"
	"example.com/app/server/internal/manager/order"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

func NewBillingManager(t *testing.T, h *scenariohost.Host) billing.BillingManager { return nil }
func NewOrderManager(t *testing.T, h *scenariohost.Host) order.OrderManager       { return nil }

func NullBillingManager(inner billing.BillingManager, ops ...string) billing.BillingManager {
	return inner
}
func NullOrderManager(inner order.OrderManager, ops ...string) order.OrderManager { return inner }
func Workflows() scenariohost.Workflows                                           { return scenariohost.Workflows{} }
`

// A binding that threads refs, arranges all four ways and probes another component compiles: the emitter's
// exhaustive switches and helper calls agree with the helpers it writes beside them.
func TestGo_RefArrangeAndProbesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	out := generateMutated(t, func(doc map[string]any) {
		withRefSteps(t, doc)
		arrangeAllKinds(doc)
	})
	root := writeModule(t, map[string]string{
		"internal/manager/billing/billing.go":                    stubBilling,
		"internal/manager/order/order.go":                        stubOrder,
		"internal/scenariocompose/compose.go":                    stubComposeBoth,
		"internal/manager/billing/manager_scenarios.gen_test.go": string(out.Generated[billingGen]),
		"internal/manager/order/manager_scenarios.gen_test.go":   string(out.Generated[orderGen]),
		billingNull: string(out.Generated[billingNull]),
		"internal/manager/order/null.gen_test.go": string(out.Generated["internal/manager/order/null.gen_test.go"]),
	}, moduleOpts{})
	if msg, err := goIn(root, nil, "vet", "./..."); err != nil {
		t.Fatalf("the generated files do not type-check: %v\n%s", err, msg)
	}
}

// shopToolsConventional is the shop-tools fixture laid out the way clientgen lays a machine client out: its
// package is the tool table internal/client/<leaf>/<manager package> of the Manager it fronts, and the System
// draws that relationship. composegen derives the client's constructor from exactly that.
func shopToolsConventional(t *testing.T, doc map[string]any) {
	t.Helper()
	withShopTools(t, doc)
	doc["serviceContracts"].(map[string]any)["shopTools"].(map[string]any)["goPackage"] = conventionalToolsPkg
	design := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	design["relationships"] = append(design["relationships"].([]any),
		map[string]any{"from": "shop-tools", "to": "order-manager", "mode": "queued", "label": "OrderGetOrder"})
}

const (
	conventionalToolsPkg = "internal/client/shoptools/order"
	conventionalToolsGen = conventionalToolsPkg + "/client_scenarios.gen_test.go"
)

// stubOrderCtor and stubBillingCtor stand in for the Managers' generated constructors (modelgen and the
// Manager's own code, which this module does not build); the composition around them is composegen's.
const stubOrderCtor = `package order

func NewOrderManager() OrderManager { return nil }
`

const stubBillingCtor = `package billing

func NewBillingManager() BillingManager { return nil }
`

// stubToolsHandler stands in for clientgen's generated tool table: the Handler type composegen's
// constructor builds (Manager + Security fields, Register on an MCP server). composegen's own compile
// test builds the real clientgen output against the same constructor.
const stubToolsHandler = `package order

import (
	mgr "example.com/app/server/internal/manager/order"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Handler struct {
	Manager  mgr.OrderManager
	Security security.Security
}

func (h *Handler) Register(srv *mcp.Server) {}
`

// The generated MCP file type-checks against the REAL composegen output for the same model: the
// scenariocompose package is composegen.EmitScenarioCompose over the fixture, not a stand-in, so a
// constructor the emitter calls that composegen does not produce, or one of another shape, fails here.
func TestMCP_GeneratedFileCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	doc := fixtureDoc(t)
	shopToolsConventional(t, doc)
	out, err := generateDoc(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	m, err := projectmodel.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	effective := *m
	effective.Deployment = m.EffectiveDeployment("server")
	compose, _, err := composegen.EmitScenarioCompose(&effective, composegen.Config{ContainerKey: "server", ModulePath: "example.com/app/server"})
	if err != nil {
		t.Fatalf("composegen.EmitScenarioCompose: %v", err)
	}
	if !strings.Contains(string(compose), "func NewShopTools(t *testing.T, h *scenariohost.Host) *") {
		t.Fatalf("composegen emits no constructor for the shopTools client:\n%s", compose)
	}
	root := writeModule(t, map[string]string{
		"internal/manager/order/order.go":     stubOrder,
		"internal/manager/order/ctor.go":      stubOrderCtor,
		"internal/manager/billing/billing.go": stubBilling,
		"internal/manager/billing/ctor.go":    stubBillingCtor,
		conventionalToolsPkg + "/tools.go":    stubToolsHandler,
		"internal/scenariocompose/compose.go": string(compose),
		conventionalToolsGen:                  string(out.Generated[conventionalToolsGen]),
	}, moduleOpts{mcp: true})
	if msg, err := goIn(root, nil, "vet", "./..."); err != nil {
		t.Fatalf("the generated MCP file does not type-check against composegen's scenariocompose: %v\n%s", err, msg)
	}
}

// The same generated files type-check against the real scenario host, not just the stub the other tests run
// them on: an arrange primitive or context method the host lacks fails here.
func TestGo_TypeChecksAgainstTheRealHost(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the real scenario host")
	}
	out := generateMutated(t, func(doc map[string]any) {
		withRefSteps(t, doc)
		arrangeAllKinds(doc)
	})
	root := writeModule(t, map[string]string{
		"internal/manager/billing/billing.go":                    stubBilling,
		"internal/manager/order/order.go":                        stubOrder,
		"internal/scenariocompose/compose.go":                    stubComposeBoth,
		"internal/manager/billing/manager_scenarios.gen_test.go": string(out.Generated[billingGen]),
		"internal/manager/order/manager_scenarios.gen_test.go":   string(out.Generated[orderGen]),
		billingNull: string(out.Generated[billingNull]),
		"internal/manager/order/null.gen_test.go": string(out.Generated["internal/manager/order/null.gen_test.go"]),
	}, moduleOpts{realHost: true})
	if msg, err := goIn(root, nil, "vet", "./..."); err != nil {
		t.Fatalf("the generated files do not type-check against the real host: %v\n%s", err, msg)
	}
}
