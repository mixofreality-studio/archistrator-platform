package transportgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/transportgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fiveManagers is the archistrator design/ops/billing manager set the fidelity
// and compile-proof tests emit an SDK for.
var fiveManagers = []string{
	"systemDesignManager", "projectDesignManager",
	"constructionManager", "operationsManager", "billingManager",
}

// TestGreenfieldGolden emits the SDK for the synthetic greenfield orderManager +
// fulfillmentManager pair and byte-compares every emitted file against committed
// goldens. The second manager (fulfillmentManager) exists solely to pin emission
// branches orderManager alone never exercises: a GET op with a path param AND a
// query param (GetShipmentStatus), a void op with zero body params — POST + `{}`
// body + 204 (PingCarrier), an int enum with x-enum-varnames (CarrierStatus, pins
// the <Type>Name varname bridge), a $def name collision with orderManager's own
// OrderID but a DIFFERENT shape (string vs integer — forces the conflict
// prefix-rename + $ref-rewrite path on BOTH managers), and a byte-identical
// shared $def (Money, declared verbatim in both managers — forces the
// types_shared.gen.go dedup path).
//
// Hand-verified against the emitter rules on introduction (2026-07):
//   - greenfield.transportgen.types_fulfillment.gen.go.golden: `type
//     FulfillmentOrderID int64` (conflict-renamed from bare "OrderID", ManagerBase
//     "Fulfillment" prefixed) plus the CarrierStatusName varname-bridge switch,
//     one case per x-enum-varnames entry in declared order, default "".
//   - greenfield.transportgen.types_order.gen.go.golden: bare "OrderID" is ALSO a
//     conflict now (fulfillmentManager declares an OrderID of a different shape,
//     integer vs string) — renamed to `OrderOrderID`, and Order.ID's $ref rewritten
//     to match; Money is skipped here (lives in types_shared.gen.go instead).
//   - greenfield.transportgen.types_shared.gen.go.golden: `Money` emitted exactly
//     once, un-prefixed, since its isolated EmitTypes output is byte-identical
//     across both declaring managers.
//   - greenfield.transportgen.http_fulfillment.gen.go.golden:
//     FulfillmentGetShipmentStatus is GET (name starts "Get", its only non-path
//     param "detail" is a scalar) with a `fmt.Sprintf(.../%s, shipmentID)` path
//     assembly plus a `url.Values`-encoded query string for "detail";
//     FulfillmentPingCarrier is POST (name doesn't start Get/List/Query) with ITS
//     path param consuming its only op param, so BodyParams is empty — the request
//     wrapper is `FulfillmentPingCarrierRequest{}` (zero fields) and the call
//     decodes no response body (`http.StatusNoContent`), pinning the void/204 +
//     empty-`{}`-body branches together, exactly as they emit in combination.
//   - greenfield.transportgen.mcp_fulfillment.gen.go.golden: mirrors the same two
//     ops; MCP input structs carry every op param (path/query distinction is an
//     HTTP-only concept) and PingCarrier's tool call passes a nil result pointer.
func TestGreenfieldGolden(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	got, err := transportgen.Generate(m, transportgen.Config{
		Managers: []string{"orderManager", "fulfillmentManager"}, PackageName: "sdk", UUIDAsString: true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Two managers sharing a byte-identical Money $def AND conflicting over a
	// differently-shaped OrderID $def: both the shared file and the per-manager
	// conflict-prefixed rename are exercised.
	want := []string{
		"types_order.gen.go", "http_order.gen.go", "mcp_order.gen.go",
		"types_fulfillment.gen.go", "http_fulfillment.gen.go", "mcp_fulfillment.gen.go",
		"types_shared.gen.go", "core.gen.go",
	}
	if len(got) != len(want) {
		t.Fatalf("Generate returned %d files, want %d (%v)", len(got), len(want), keysOf(got))
	}
	for _, name := range want {
		src, ok := got[name]
		if !ok {
			t.Fatalf("Generate did not return %q (%v)", name, keysOf(got))
		}
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors); err != nil {
			t.Errorf("emitted %s does not parse: %v", name, err)
		}
		checkGolden(t, filepath.Join("../testdata", "greenfield.transportgen."+name+".golden"), src)
	}
}

// TestArchistratorRouteFidelity emits the SDK route table for the 5 managers and
// asserts every one of the 23 verb+path routes transcribed from archistrator's
// hand systemtests transport (httptransport.go) is bound identically — the
// byte-exact mirror proof that the generated client speaks the same wire as the
// server binds.
func TestArchistratorRouteFidelity(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	routes, err := transportgen.RouteTable(m, fiveManagers)
	if err != nil {
		t.Fatalf("route table: %v", err)
	}
	got := map[string]bool{}
	for _, r := range routes {
		got[r.Verb+" "+r.Path] = true
	}

	// Transcribed verbatim from systemtests/internal/harness/httptransport.go.
	want := []string{
		// UC1 system-design
		"POST /api/v1/system-design/create-project",
		"GET /api/v1/system-design/list-projects?owner={owner}",
		"POST /api/v1/system-design/set-research-input/{projectID}",
		"POST /api/v1/system-design/start-system-design/{projectID}",
		"POST /api/v1/system-design/request-artifact-draft/{projectID}",
		"GET /api/v1/system-design/get-session-state/{projectID}?kind={kind}",
		"POST /api/v1/system-design/submit-review-decision/{projectID}",
		"POST /api/v1/system-design/advance-phase/{projectID}",
		// UC2 project-design
		"POST /api/v1/project-design/request-artifact-draft/{projectID}",
		"GET /api/v1/project-design/get-session-state/{projectID}?kind={kind}",
		"POST /api/v1/project-design/submit-review-decision/{projectID}",
		"POST /api/v1/project-design/request-sdp-commit/{projectID}",
		"POST /api/v1/project-design/submit-sdp-decision/{projectID}/{optionID}",
		"POST /api/v1/project-design/advance-to-construction/{projectID}",
		// UC3 construction
		"POST /api/v1/construction/execute-next-activity/{projectID}",
		"GET /api/v1/construction/get-session-state/{projectID}/{activityID}",
		"POST /api/v1/construction/submit-phase-decision/{projectID}/{activityID}",
		"POST /api/v1/construction/update-review-policy/{projectID}",
		// UC4 operations
		"POST /api/v1/operations/deploy-after-construction/{operatedAppID}",
		"POST /api/v1/operations/reconcile-operated-state",
		"GET /api/v1/operations/query-operated-system-view/{operatedAppID}?requestID={requestID}",
		"POST /api/v1/operations/apply-delinquency-policy/{customerID}",
		"POST /api/v1/operations/withdraw-system/{operatedAppID}",
	}
	if len(want) != 23 {
		t.Fatalf("expectation table has %d entries, want 23", len(want))
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("route fidelity: generated table is MISSING %q", w)
		}
	}
}

// TestArchistratorCompileSandbox emits the full 5-manager SDK into an isolated
// throwaway module and proves (a) it builds with `go build` under GOWORK=off,
// (b) every emitted file imports NOTHING beyond the Go standard library, and
// (c) it passes `go vet` — the gate that catches the printf-verb defect class
// where an optional (pointer) path param is formatted into a path template
// with a plain %s/%d/%v verb (see writePathAssembly / pathParamSet): a path
// segment is always present on the wire, so path params must be value
// scalars in the generated signature regardless of the contract's
// PlanParam.Pointer, or vet's printf checker rejects the mismatch.
func TestArchistratorCompileSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile sandbox in -short")
	}
	m, err := projectmodel.LoadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	out, err := transportgen.Generate(m, transportgen.Config{
		Managers: fiveManagers, PackageName: "sdk", UUIDAsString: true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module sdksandbox\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for name, src := range out {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !isStdlib(path) {
				t.Errorf("%s imports non-stdlib %q (the SDK must be zero-dependency)", name, path)
			}
		}
	}

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in sandbox failed: %v\n%s", err, buildOut)
	}

	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Dir = dir
	vetCmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if vetOut, err := vetCmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet ./... in sandbox failed: %v\n%s", err, vetOut)
	}
}

// TestArchistratorPathParamsAreValueScalars pins the fix for the pointer-path
// printf defect: an op whose contract marks an ID path param optional
// (PlanParam.Pointer) must still emit that param as a VALUE scalar — never a
// pointer — in both the HTTP client method signature and the MCP input
// struct/signature, since a path segment is always present on the wire.
// ConstructionGetSessionState's activityID and ProjectDesignSubmitSDPDecision's
// optionID are both declared `"pointer": true` in archistrator.project.json
// while being path params, so they exercise exactly the regressed branch.
func TestArchistratorPathParamsAreValueScalars(t *testing.T) {
	m, err := projectmodel.LoadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	out, err := transportgen.Generate(m, transportgen.Config{
		Managers: fiveManagers, PackageName: "sdk", UUIDAsString: true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	checks := []struct {
		file string
		want string
		bad  string
	}{
		{"http_construction.gen.go", "activityID ActivityID", "activityID *ActivityID"},
		{"http_project-design.gen.go", "optionID OptionID", "optionID *OptionID"},
		{"mcp_construction.gen.go", "activityID ActivityID", "activityID *ActivityID"},
		{"mcp_project-design.gen.go", "optionID OptionID", "optionID *OptionID"},
	}
	for _, c := range checks {
		src, ok := out[c.file]
		if !ok {
			t.Fatalf("Generate did not return %q (%v)", c.file, keysOf(out))
		}
		s := string(src)
		if !strings.Contains(s, c.want) {
			t.Errorf("%s: want value-scalar param %q, not found in:\n%s", c.file, c.want, s)
		}
		if strings.Contains(s, c.bad) {
			t.Errorf("%s: found regressed pointer param %q", c.file, c.bad)
		}
	}
}

// TestConfigErrors asserts Generate rejects an empty package name / manager set.
func TestConfigErrors(t *testing.T) {
	m := &projectmodel.Model{Contracts: map[string]*projectmodel.Contract{}}
	if _, err := transportgen.Generate(m, transportgen.Config{Managers: []string{"x"}}); err == nil || !strings.Contains(err.Error(), "PackageName") {
		t.Errorf("want PackageName error, got %v", err)
	}
	if _, err := transportgen.Generate(m, transportgen.Config{PackageName: "sdk"}); err == nil || !strings.Contains(err.Error(), "Managers") {
		t.Errorf("want Managers error, got %v", err)
	}
}

// isStdlib reports whether an import path is a Go standard-library package: its
// first path segment carries no dot (only external module paths are domains).
func isStdlib(path string) bool {
	seg := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		seg = path[:i]
	}
	return !strings.Contains(seg, ".")
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("output mismatch for %s (run with -update to refresh)", path)
	}
}
