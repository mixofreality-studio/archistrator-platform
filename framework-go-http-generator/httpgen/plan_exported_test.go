package httpgen_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// TestPlanOps exercises the exported PlanOps surface against the local
// operations.contract.schema.json fixture (already used by TestGenerateGolden
// above). That single contract happens to carry all three cases the plan
// calls for in one place:
//   - QueryOperatedSystemView is BOTH an ID-path op (operatedAppID, an inline
//     uuid whose param name ends "ID") AND a GET-with-query op (requestID, a
//     plain string, rides the query string because the op name starts
//     "Query" and every non-path param is scalar).
//   - ApplyDelinquencyPolicy is a void op (no result) with an ID path param
//     (customerID) and a body param (delinquencyContext, an object — POST).
//
// Using this in-module fixture (rather than the app-generator's greenfield
// fixture) avoids a cross-module testdata reference: greenfield's ops
// (PlaceOrder, ReadOrder, PutOrder, ...) don't include a Get/List/Query op at
// all, so it would need local extension anyway — and this fixture already
// covers every case the plan asks PlanOps to prove out.
func TestPlanOps(t *testing.T) {
	raw, err := os.ReadFile("../testdata/operations.contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := projectmodel.Parse(raw)
	if err != nil {
		t.Fatalf("parse contract: %v", err)
	}

	plans, err := httpgen.PlanOps(doc)
	if err != nil {
		t.Fatalf("PlanOps: %v", err)
	}
	if len(plans) != len(doc.Interface.Operations) {
		t.Fatalf("PlanOps returned %d plans, want %d (one per op)", len(plans), len(doc.Interface.Operations))
	}

	byName := make(map[string]httpgen.OpPlan, len(plans))
	for _, p := range plans {
		byName[p.Name] = p
	}

	t.Run("ID-path op with GET query (QueryOperatedSystemView)", func(t *testing.T) {
		p, ok := byName["QueryOperatedSystemView"]
		if !ok {
			t.Fatal("no plan for QueryOperatedSystemView")
		}
		if p.Verb != "GET" {
			t.Errorf("Verb = %q, want GET", p.Verb)
		}
		wantPath := "/api/v1/operations/query-operated-system-view/{operatedAppID}"
		if p.PathTemplate != wantPath {
			t.Errorf("PathTemplate = %q, want %q", p.PathTemplate, wantPath)
		}
		if !p.HasResult {
			t.Error("HasResult = false, want true")
		}
		if len(p.PathParams) != 1 || p.PathParams[0].Name != "operatedAppID" {
			t.Fatalf("PathParams = %+v, want [operatedAppID]", p.PathParams)
		}
		if p.PathParams[0].ScalarKind != "uuid" {
			t.Errorf("PathParams[0].ScalarKind = %q, want uuid", p.PathParams[0].ScalarKind)
		}
		if p.PathParams[0].RefName != "" {
			t.Errorf("PathParams[0].RefName = %q, want \"\" (inline uuid, no $def)", p.PathParams[0].RefName)
		}
		if len(p.QueryParams) != 1 || p.QueryParams[0].Name != "requestID" {
			t.Fatalf("QueryParams = %+v, want [requestID]", p.QueryParams)
		}
		if p.QueryParams[0].ScalarKind != "string" {
			t.Errorf("QueryParams[0].ScalarKind = %q, want string", p.QueryParams[0].ScalarKind)
		}
		if len(p.BodyParams) != 0 {
			t.Errorf("BodyParams = %+v, want none (GET carries no body)", p.BodyParams)
		}
	})

	t.Run("void op with ID path + body (ApplyDelinquencyPolicy)", func(t *testing.T) {
		p, ok := byName["ApplyDelinquencyPolicy"]
		if !ok {
			t.Fatal("no plan for ApplyDelinquencyPolicy")
		}
		if p.Verb != "POST" {
			t.Errorf("Verb = %q, want POST", p.Verb)
		}
		wantPath := "/api/v1/operations/apply-delinquency-policy/{customerID}"
		if p.PathTemplate != wantPath {
			t.Errorf("PathTemplate = %q, want %q", p.PathTemplate, wantPath)
		}
		if p.HasResult {
			t.Error("HasResult = true, want false (void op)")
		}
		if len(p.PathParams) != 1 || p.PathParams[0].Name != "customerID" {
			t.Fatalf("PathParams = %+v, want [customerID]", p.PathParams)
		}
		if len(p.QueryParams) != 0 {
			t.Errorf("QueryParams = %+v, want none (POST carries no query)", p.QueryParams)
		}
		if len(p.BodyParams) != 1 || p.BodyParams[0].Name != "delinquencyContext" {
			t.Fatalf("BodyParams = %+v, want [delinquencyContext]", p.BodyParams)
		}
		if p.BodyParams[0].RefName != "DelinquencyContext" {
			t.Errorf("BodyParams[0].RefName = %q, want DelinquencyContext", p.BodyParams[0].RefName)
		}
	})

	// Cross-check every planned route against Generate's own Register mount
	// (the same doc, same convention) so PlanOps can never silently drift
	// from what the handler emitter actually binds.
	res, err := httpgen.Generate(doc, httpgen.Options{ManagerImport: "example.com/mgr"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	handlers := string(res.HandlersGo)
	for _, p := range plans {
		route := p.Verb + " " + p.PathTemplate
		if !strings.Contains(handlers, route) {
			t.Errorf("Generate does not mount PlanOps route %q for op %q", route, p.Name)
		}
	}
}
