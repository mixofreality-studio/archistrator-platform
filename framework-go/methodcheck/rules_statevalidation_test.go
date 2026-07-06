package methodcheck

import "testing"

// rules_statevalidation_test.go covers the state-validation twins (rules_statevalidation.go),
// the authoritative platform twins of the app-side state-validation rules (app commit
// a19a25b). Each test isolates one rule: a violating fixture that fires it (with the
// expected severity) and a clean fixture that does not.

// ---- SYS-RA-ORPHAN ----

func TestRAOrphan_NoResourceEdgeFires(t *testing.T) {
	ra := comp(t, "StateAccess", kindResourceAccess)
	mgr := comp(t, "CoreManager", kindManager)
	s := System{Components: []Component{mgr, ra}, Relationships: []Relationship{{From: ra.ID, To: mgr.ID, Mode: modeSync}}}
	out := raOrphan(s)
	if !hasRuleFindings(out, ruleSysRAOrphan) {
		t.Fatalf("expected SYS-RA-ORPHAN, got %+v", out)
	}
	if sev, _ := findingSeverity(out, ruleSysRAOrphan); sev != SeverityError {
		t.Fatalf("SYS-RA-ORPHAN must be Error")
	}
}

func TestRAOrphan_ReachesResourcePasses(t *testing.T) {
	ra := comp(t, "StateAccess", kindResourceAccess)
	db := comp(t, "StateDB", kindResource)
	s := System{Components: []Component{ra, db}, Relationships: []Relationship{{From: ra.ID, To: db.ID, Mode: modeSync}}}
	if out := raOrphan(s); len(out) != 0 {
		t.Fatalf("RA reaching a resource is not an orphan, got %+v", out)
	}
}

func TestRAOrphan_ExternalTargetPasses(t *testing.T) {
	ra := comp(t, "StateAccess", kindResourceAccess)
	// The target is not a modeled component → treated as a documented external system.
	s := System{Components: []Component{ra}, Relationships: []Relationship{{From: ra.ID, To: "github-external", Mode: modeSync}}}
	if out := raOrphan(s); len(out) != 0 {
		t.Fatalf("RA reaching an external system is not an orphan, got %+v", out)
	}
}

// ---- SYS-ENCAPSULATES (the severity reconciliation) ----

func TestEncapsulates_VolatilityOwningKindsAreError(t *testing.T) {
	for _, kind := range []string{kindManager, kindEngine, kindResourceAccess} {
		c := Component{ID: "x", Name: "X", Kind: kind, Layer: kind, Encapsulates: ""}
		out := encapsulates(System{Components: []Component{c}})
		sev, ok := findingSeverity(out, ruleSysEncapsulates)
		if !ok {
			t.Fatalf("kind %q: expected SYS-ENCAPSULATES, got %+v", kind, out)
		}
		if sev != SeverityError {
			t.Fatalf("kind %q: empty encapsulates must be Error (hard write-block parity), got %v", kind, sev)
		}
	}
}

func TestEncapsulates_ClientResourceUtilityAreWarning(t *testing.T) {
	// The reconciliation: client (which the app treats as an ERROR display finding that
	// never hard-fails a read) is a WARNING here so methodcheck's blocking ERROR never
	// trips on committed state that legitimately carries empty-encapsulates clients.
	for _, kind := range []string{kindClient, kindResource, kindUtility} {
		c := Component{ID: "x", Name: "X", Kind: kind, Layer: kind, Encapsulates: ""}
		out := encapsulates(System{Components: []Component{c}})
		sev, ok := findingSeverity(out, ruleSysEncapsulates)
		if !ok {
			t.Fatalf("kind %q: expected SYS-ENCAPSULATES warning, got %+v", kind, out)
		}
		if sev != SeverityWarning {
			t.Fatalf("kind %q: empty encapsulates must be Warning (non-blocking), got %v", kind, sev)
		}
	}
}

func TestEncapsulates_NonEmptyPasses(t *testing.T) {
	c := comp(t, "CoreManager", kindManager) // comp() populates Encapsulates
	if out := encapsulates(System{Components: []Component{c}}); len(out) != 0 {
		t.Fatalf("non-empty encapsulates must not fire, got %+v", out)
	}
}

// ---- SYS-REL-DUP ----

func TestRelDup_ExactDuplicateIsError(t *testing.T) {
	rel := Relationship{From: "a", To: "b", Mode: modeSync, Label: "call"}
	out := relDup(System{Relationships: []Relationship{rel, rel}})
	sev, ok := findingSeverity(out, ruleSysRelDup)
	if !ok || sev != SeverityError {
		t.Fatalf("exact duplicate must be SYS-REL-DUP Error, got %+v", out)
	}
}

func TestRelDup_LabelSplitIsWarning(t *testing.T) {
	out := relDup(System{Relationships: []Relationship{
		{From: "a", To: "b", Mode: modeSync, Label: "read"},
		{From: "a", To: "b", Mode: modeSync, Label: "write"},
	}})
	// Same (from,to,mode) with different labels → exact-dup on mode fires ERROR (the
	// mode collides). A true label-split warning requires differing modes.
	if !hasRuleFindings(out, ruleSysRelDup) {
		t.Fatalf("expected SYS-REL-DUP, got %+v", out)
	}
}

func TestRelDup_LabelSplitAcrossModesIsWarning(t *testing.T) {
	out := relDup(System{Relationships: []Relationship{
		{From: "a", To: "b", Mode: modeSync, Label: "read"},
		{From: "a", To: "b", Mode: modeQueued, Label: "write"},
	}})
	sev, ok := findingSeverity(out, ruleSysRelDup)
	if !ok || sev != SeverityWarning {
		t.Fatalf("label-split across distinct modes must be SYS-REL-DUP Warning, got %+v", out)
	}
}

func TestRelDup_SingleEdgePasses(t *testing.T) {
	out := relDup(System{Relationships: []Relationship{{From: "a", To: "b", Mode: modeSync}}})
	if len(out) != 0 {
		t.Fatalf("a single edge is not a duplicate, got %+v", out)
	}
}

// ---- DV-CHAIN-CONNECTED ----

func TestDVChain_NoClientRootWarns(t *testing.T) {
	mgr := comp(t, "M", kindManager)
	eng := comp(t, "E", kindEngine)
	s := System{
		Components:   []Component{mgr, eng},
		DynamicViews: []DynamicView{{Key: "uc1", Participants: []string{mgr.ID, eng.ID}, Edges: []Relationship{{From: mgr.ID, To: eng.ID, Mode: modeSync}}}},
	}
	sev, ok := findingSeverity(dvChainConnected(s), ruleDVChainConn)
	if !ok || sev != SeverityWarning {
		t.Fatalf("a chain with no Client root must warn DV-CHAIN-CONNECTED")
	}
}

func TestDVChain_DisconnectedWarns(t *testing.T) {
	client := comp(t, "C", kindClient)
	mgr := comp(t, "M", kindManager)
	eng := comp(t, "E", kindEngine)
	s := System{
		Components: []Component{client, mgr, eng},
		DynamicViews: []DynamicView{{Key: "uc1",
			Participants: []string{client.ID, mgr.ID, eng.ID},
			Edges:        []Relationship{{From: client.ID, To: mgr.ID, Mode: modeSync}}, // eng unreachable
		}},
	}
	if !hasRuleFindings(dvChainConnected(s), ruleDVChainConn) {
		t.Fatalf("an unreachable participant must warn DV-CHAIN-CONNECTED")
	}
}

func TestDVChain_ConnectedPasses(t *testing.T) {
	client := comp(t, "C", kindClient)
	mgr := comp(t, "M", kindManager)
	s := System{
		Components:   []Component{client, mgr},
		DynamicViews: []DynamicView{{Key: "uc1", Participants: []string{client.ID, mgr.ID}, Edges: []Relationship{{From: client.ID, To: mgr.ID, Mode: modeSync}}}},
	}
	if out := dvChainConnected(s); len(out) != 0 {
		t.Fatalf("a connected chain must not warn, got %+v", out)
	}
}

// ---- UC-ACT-PRESENT ----

func TestUCActPresent_NilActivityFires(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{Name: "X", Classification: classCore}}}}
	if !hasRuleFindings(ucActPresent(c), ruleUCActPresent) {
		t.Fatalf("a nil activity must fire UC-ACT-PRESENT")
	}
}

func TestUCActPresent_StructurallyEmptyFires(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{Name: "X", Classification: classCore,
		Activity: &ActivityDiagram{Nodes: []ActivityNode{{ID: "d", Kind: nodeDecision}}}}}}}
	if !hasRuleFindings(ucActPresent(c), ruleUCActPresent) {
		t.Fatalf("an activity with no start+action must fire UC-ACT-PRESENT")
	}
}

func TestUCActPresent_ValidPasses(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{coreUC("X")}} // coreUC carries a minimal activity
	if out := ucActPresent(c); len(out) != 0 {
		t.Fatalf("a start+action activity must pass, got %+v", out)
	}
}

// ---- UC-GUARD-LABEL ----

func TestUCGuardLabel_EmptyGuardFires(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{Name: "X", Classification: classCore,
		Activity: &ActivityDiagram{Edges: []ActivityEdge{{From: "d", To: "a", Kind: edgeGuardedFlow, Guard: ""}}}}}}}
	if !hasRuleFindings(ucGuardLabel(c), ruleUCGuardLabel) {
		t.Fatalf("a guardedFlow edge with empty guard must fire UC-GUARD-LABEL")
	}
}

func TestUCGuardLabel_LabeledGuardPasses(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{Name: "X", Classification: classCore,
		Activity: &ActivityDiagram{Edges: []ActivityEdge{
			{From: "d", To: "a", Kind: edgeGuardedFlow, Guard: "amount > 0"},
			{From: "s", To: "d", Kind: edgeControlFlow}, // plain edge needs no guard
		}}}}}}
	if out := ucGuardLabel(c); len(out) != 0 {
		t.Fatalf("a labeled guard must pass, got %+v", out)
	}
}

// ---- UC-VARIATION-REF ----

func TestVariationRef_CoreWithVariationOfFires(t *testing.T) {
	ref := "base"
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{ID: "base", Name: "Base", Classification: classCore, VariationOf: &ref}}}}
	if !hasRuleFindings(variationRef(c), ruleUCVariationRef) {
		t.Fatalf("a core use case with a variationOf must fire UC-VARIATION-REF")
	}
}

func TestVariationRef_NonCoreMissingVariationOfFires(t *testing.T) {
	c := CoreUseCases{Decisions: []UseCaseDecision{{UseCase: UseCase{ID: "v", Name: "V", Classification: "nonCore"}, RejectionReason: "why"}}}
	if !hasRuleFindings(variationRef(c), ruleUCVariationRef) {
		t.Fatalf("a nonCore use case with no variationOf must fire UC-VARIATION-REF")
	}
}

func TestVariationRef_NonCoreUnresolvedRefFires(t *testing.T) {
	ref := "does-not-exist"
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: "base", Name: "Base", Classification: classCore}},
		{UseCase: UseCase{ID: "v", Name: "V", Classification: "nonCore", VariationOf: &ref}, RejectionReason: "why"},
	}}
	if !hasRuleFindings(variationRef(c), ruleUCVariationRef) {
		t.Fatalf("a nonCore variationOf that does not resolve to a core use case must fire UC-VARIATION-REF")
	}
}

func TestVariationRef_NonCoreEmptyRejectionFires(t *testing.T) {
	ref := "base"
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: "base", Name: "Base", Classification: classCore}},
		{UseCase: UseCase{ID: "v", Name: "V", Classification: "nonCore", VariationOf: &ref}, RejectionReason: ""},
	}}
	if !hasRuleFindings(variationRef(c), ruleUCVariationRef) {
		t.Fatalf("a nonCore use case with an empty rejectionReason must fire UC-VARIATION-REF")
	}
}

func TestVariationRef_ValidPasses(t *testing.T) {
	ref := "base"
	c := CoreUseCases{Decisions: []UseCaseDecision{
		{UseCase: UseCase{ID: "base", Name: "Base", Classification: classCore}},
		{UseCase: UseCase{ID: "v", Name: "V", Classification: "nonCore", VariationOf: &ref}, RejectionReason: "narrower scope"},
	}}
	if out := variationRef(c); len(out) != 0 {
		t.Fatalf("a well-formed variation must pass, got %+v", out)
	}
}

// ---- VOL-AXIS-EXPLICIT ----

func TestVolAxisExplicit_MissingFires(t *testing.T) {
	v := Volatilities{Items: []Volatility{{Name: "X", Axis: ""}}}
	sev, ok := findingSeverity(volAxisExplicit(v), ruleVolAxisExplicit)
	if !ok || sev != SeverityError {
		t.Fatalf("a missing axis must fire VOL-AXIS-EXPLICIT Error, got %+v", volAxisExplicit(v))
	}
}

func TestVolAxisExplicit_UnrecognizedFires(t *testing.T) {
	v := Volatilities{Items: []Volatility{{Name: "X", Axis: "sideways"}}}
	if !hasRuleFindings(volAxisExplicit(v), ruleVolAxisExplicit) {
		t.Fatalf("an unrecognized axis must fire VOL-AXIS-EXPLICIT")
	}
}

func TestVolAxisExplicit_ValidPasses(t *testing.T) {
	v := Volatilities{Items: []Volatility{{Name: "X", Axis: axisSameCustomerOverTime}, {Name: "Y", Axis: axisAllCustomersAtOneTime}}}
	if out := volAxisExplicit(v); len(out) != 0 {
		t.Fatalf("explicit recognized axes must pass, got %+v", out)
	}
}

// ---- STD-STATUS-EXPLICIT ----

func TestStdStatusExplicit_MissingFires(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Guideline: "g", Status: ""}}}
	sev, ok := findingSeverity(stdStatusExplicit(sc), ruleStdStatusExplicit)
	if !ok || sev != SeverityError {
		t.Fatalf("a missing status must fire STD-STATUS-EXPLICIT Error")
	}
}

func TestStdStatusExplicit_ValidPasses(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Guideline: "g", Status: checkPass}, {Guideline: "h", Status: checkWaived, Justification: "j"}}}
	if out := stdStatusExplicit(sc); len(out) != 0 {
		t.Fatalf("explicit recognized statuses must pass, got %+v", out)
	}
}

// ---- STD-FAIL-OPEN ----

func TestStdFailOpen_FailItemFires(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Guideline: "g", Status: checkFail}}}
	sev, ok := findingSeverity(stdFailOpen(sc), ruleStdFailOpen)
	if !ok || sev != SeverityError {
		t.Fatalf("a committed fail item must fire STD-FAIL-OPEN Error")
	}
}

func TestStdFailOpen_PassAndWaivedPass(t *testing.T) {
	sc := StandardCheck{Items: []CheckItem{{Guideline: "g", Status: checkPass}, {Guideline: "h", Status: checkWaived, Justification: "j"}}}
	if out := stdFailOpen(sc); len(out) != 0 {
		t.Fatalf("pass/waived items must not fire STD-FAIL-OPEN, got %+v", out)
	}
}

// ---- GLOSS-FOURQ ----

func TestGlossFourQ_NonCanonicalCategoryIsError(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "T", Category: "Nonsense"}}}
	sev, ok := findingSeverity(glossFourQ(g), ruleGlossFourQ)
	if !ok || sev != SeverityError {
		t.Fatalf("a non-canonical category must fire GLOSS-FOURQ Error")
	}
}

func TestGlossFourQ_MissingCoverageIsWarning(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{{Term: "T", Category: "Who"}}} // only Who covered
	out := glossFourQ(g)
	if !hasRuleFindings(out, ruleGlossFourQ) {
		t.Fatalf("missing question coverage must warn GLOSS-FOURQ")
	}
	for _, f := range out {
		if f.Severity == SeverityError {
			t.Fatalf("coverage gaps must be warnings, not errors: %+v", f)
		}
	}
}

func TestGlossFourQ_FullCoveragePasses(t *testing.T) {
	g := Glossary{Items: []GlossaryItem{
		{Term: "A", Category: "Who"}, {Term: "B", Category: "What"},
		{Term: "C", Category: "How"}, {Term: "D", Category: "Where"},
	}}
	if out := glossFourQ(g); len(out) != 0 {
		t.Fatalf("full canonical coverage must pass, got %+v", out)
	}
}

// ---- SR-ID-UNIQUE ----

func TestSRIDUnique_EmptyIDFires(t *testing.T) {
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "", Statement: "s"}}}
	if !hasRuleFindings(srIDUnique(sr), ruleSRIDUnique) {
		t.Fatalf("an empty id must fire SR-ID-UNIQUE")
	}
}

func TestSRIDUnique_DuplicateIDFires(t *testing.T) {
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "a"}, {ID: "R1", Statement: "b"}}}
	if !hasRuleFindings(srIDUnique(sr), ruleSRIDUnique) {
		t.Fatalf("a duplicate id must fire SR-ID-UNIQUE")
	}
}

func TestSRIDUnique_EmptyStatementFires(t *testing.T) {
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: ""}}}
	if !hasRuleFindings(srIDUnique(sr), ruleSRIDUnique) {
		t.Fatalf("an empty statement must fire SR-ID-UNIQUE")
	}
}

func TestSRIDUnique_ValidPasses(t *testing.T) {
	sr := ScrubbedRequirements{Items: []Requirement{{ID: "R1", Statement: "a"}, {ID: "R2", Statement: "b"}}}
	if out := srIDUnique(sr); len(out) != 0 {
		t.Fatalf("unique non-empty requirements must pass, got %+v", out)
	}
}

// ---- OPC-TOPIC-COVERAGE ----

func TestOPCTopicCoverage_MissingTopicIsInfo(t *testing.T) {
	o := OperationalConcepts{Decisions: []OperationalDecision{{Topic: "topology", Decision: "d"}}}
	out := opcTopicCoverage(o)
	if !hasRuleFindings(out, ruleOPCTopicCoverage) {
		t.Fatalf("uncovered canonical topics must emit OPC-TOPIC-COVERAGE")
	}
	for _, f := range out {
		if f.Severity != SeverityInfo {
			t.Fatalf("OPC-TOPIC-COVERAGE must be Info, got %v", f.Severity)
		}
	}
}

func TestOPCTopicCoverage_AllTopicsPass(t *testing.T) {
	o := OperationalConcepts{Decisions: []OperationalDecision{
		{Topic: "topology"}, {Topic: "sync vs queued"}, {Topic: "layering style"}, {Topic: "state handling"},
	}}
	if out := opcTopicCoverage(o); len(out) != 0 {
		t.Fatalf("full topic coverage must pass, got %+v", out)
	}
}
