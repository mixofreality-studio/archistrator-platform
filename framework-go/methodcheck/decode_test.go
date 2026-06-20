package methodcheck

import (
	"os"
	"path/filepath"
	"testing"
)

// decode_test.go exercises DecodeProject over a REAL committed project.json fixture
// (testdata/project.json — captured verbatim from the aiarch projectstate
// EncodeProjectJSON codec) so the structural decode is proven against the exact
// on-disk shape the server writes, not a hand-guess.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func TestDecodeProject_RealFixtureRoundTrip(t *testing.T) {
	p, ok, err := DecodeProject(readFixture(t, "project.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a non-empty document")
	}
	if p.ID != "demo" {
		t.Fatalf("id: got %q want demo", p.ID)
	}
	if n := p.committedSlotCount(); n != 8 {
		t.Fatalf("committed Phase-1 slots: got %d want 8", n)
	}

	// Mission decodes with its objectives.
	m, mok, err := p.mission()
	if err != nil || !mok {
		t.Fatalf("mission: ok=%v err=%v", mok, err)
	}
	if m.Vision != "v" || len(m.Objectives) != 2 || m.Objectives[0].Number != 1 {
		t.Fatalf("mission decoded wrong: %+v", m)
	}

	// Volatilities decode their string-enum axis.
	v, _, err := p.volatilities()
	if err != nil {
		t.Fatalf("volatilities: %v", err)
	}
	if len(v.Items) != 2 || v.Items[0].Axis != axisSameCustomerOverTime || v.Items[1].Axis != axisAllCustomersAtOneTime {
		t.Fatalf("volatilities axis decoded wrong: %+v", v.Items)
	}

	// System decodes components with string-enum kind + layer and the activity diagram.
	s, _, err := p.system()
	if err != nil {
		t.Fatalf("system: %v", err)
	}
	if len(s.Components) != 5 {
		t.Fatalf("components: got %d want 5", len(s.Components))
	}
	if s.Components[1].Kind != kindManager || s.Components[1].Layer != layerManager {
		t.Fatalf("manager component decoded wrong: %+v", s.Components[1])
	}
	if len(s.DynamicViews) != 2 || s.DynamicViews[0].Key != "uc1" {
		t.Fatalf("dynamic views decoded wrong: %+v", s.DynamicViews)
	}

	// Core use cases decode the nested activity diagram with guarded edges.
	c, _, err := p.coreUseCases()
	if err != nil {
		t.Fatalf("coreUseCases: %v", err)
	}
	act := c.Decisions[0].UseCase.Activity
	if act == nil || len(act.Nodes) != 4 || act.Edges[0].Kind != edgeGuardedFlow {
		t.Fatalf("activity diagram decoded wrong: %+v", act)
	}

	// Operational concepts decode the deployment topology string enums.
	o, _, err := p.operationalConcepts()
	if err != nil {
		t.Fatalf("operationalConcepts: %v", err)
	}
	if o.Deployment.DeliveryStyle != styleBoth || o.Deployment.Environments[0].Profile != profileCloud {
		t.Fatalf("deployment enums decoded wrong: %+v", o.Deployment)
	}

	// Standard check decodes the waived status.
	sc, _, err := p.standardCheck()
	if err != nil {
		t.Fatalf("standardCheck: %v", err)
	}
	if len(sc.Items) != 2 || sc.Items[1].Status != checkWaived {
		t.Fatalf("standard check decoded wrong: %+v", sc.Items)
	}
}

func TestDecodeProject_EmptyIsNotAnError(t *testing.T) {
	_, ok, err := DecodeProject(nil)
	if err != nil {
		t.Fatalf("empty input must not error: %v", err)
	}
	if ok {
		t.Fatal("empty input must yield ok=false")
	}
}

func TestDecodeProject_UncommittedSlotIsNotReadable(t *testing.T) {
	// status 1 (AwaitingReview) must NOT count as committed.
	raw := []byte(`{"id":"x","slots":{"0":{"status":1,"kind":0,"model":{"vision":"v"}}}}`)
	p, ok, err := DecodeProject(raw)
	if err != nil || !ok {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if _, mok, _ := p.mission(); mok {
		t.Fatal("an AwaitingReview slot must not be readable as committed")
	}
	if p.committedSlotCount() != 0 {
		t.Fatal("an uncommitted slot must not count")
	}
}

// TestValidateProjectJSON_CleanFixturePasses proves the orchestration produces zero
// Error findings over a fully-legal committed project (testdata/project_clean.json).
func TestValidateProjectJSON_CleanFixturePasses(t *testing.T) {
	findings, err := ValidateProjectJSON(readFixture(t, "project_clean.json"))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, f := range findings {
		if f.Severity == SeverityError {
			t.Fatalf("clean fixture produced an Error finding: %s %s", f.RuleID, f.Message)
		}
	}
}

// TestValidateProjectJSON_RealFixtureFlagsDeploymentGaps proves the real fixture
// (whose committed deployment topology only declares the cloud env under StyleBoth)
// trips the deployment Error rules — a concrete cross-artifact-rule exercise over the
// decoded real shape.
func TestValidateProjectJSON_RealFixtureFlagsDeploymentGaps(t *testing.T) {
	findings, err := ValidateProjectJSON(readFixture(t, "project.json"))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !hasRuleFindings(findings, ruleDepProfileSet) {
		t.Fatalf("expected DEP-PROFILE-SET over the real fixture's single-env deployment, got %+v", findings)
	}
}
