package transportgen

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// fiveManagersForRouteFidelity mirrors fiveManagers in transportgen_test.go.
// Duplicated (rather than shared) because this file is an INTERNAL test
// (package transportgen) — it needs access to the unexported routeTable
// helper, which an external _test package cannot reach, and an internal test
// file cannot see an unexported var declared in a different (_test) package.
var fiveManagersForRouteFidelity = []string{
	"systemDesignManager", "projectDesignManager",
	"constructionManager", "operationsManager", "billingManager",
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
	routes, err := routeTable(m, fiveManagersForRouteFidelity)
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
