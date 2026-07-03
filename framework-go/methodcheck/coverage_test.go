package methodcheck

import "testing"

func TestDefaultCoverage_NoSilentGaps(t *testing.T) {
	items := DefaultCoverage()
	if len(items) == 0 {
		t.Fatal("DefaultCoverage() returned empty slice; expected ~80 App-C items")
	}
	for _, item := range items {
		if item.AppcRef == "" {
			t.Errorf("item with empty AppcRef: %+v", item)
		}
		// Only AUTOMATED items must bind a rule ID. Human-judgment and permission
		// items legitimately carry none (there is nothing to automate).
		if item.Classification.isAutomated() && item.RuleID == "" {
			t.Errorf("appcRef=%q is automated but has no RuleID", item.AppcRef)
		}
		if !item.Classification.isAutomated() && item.RuleID != "" {
			t.Errorf("appcRef=%q is non-automated (%q) but carries RuleID=%q; a non-automated item must not claim a rule", item.AppcRef, item.Classification, item.RuleID)
		}
		if item.Kind != AppCDirective && item.Kind != AppCGuideline {
			t.Errorf("appcRef=%q has invalid Kind=%q", item.AppcRef, item.Kind)
		}
	}
}

// TestDefaultCoverage_EveryAutomatedRuleHasEmitter is the platform's own guard
// against a phantom rule: every coverage item classified automated must bind a
// RuleID that the emitters.go registry lists as actually emitted. A matrixed rule
// with no emitter — the SYS-5a/b/c mistake — fails here instead of masquerading as
// enforced. It is the inverse-completeness partner to the coverage matrix.
func TestDefaultCoverage_EveryAutomatedRuleHasEmitter(t *testing.T) {
	emitted := emittedRuleIDs()
	for _, item := range DefaultCoverage() {
		if !item.Classification.isAutomated() {
			continue
		}
		if !emitted[item.RuleID] {
			t.Errorf("appcRef=%q is classified %q with RuleID=%q, but no emitter is registered for it in emitters.go (a coverage-matrix rule with no emitter is not actually enforced)", item.AppcRef, item.Classification, item.RuleID)
		}
	}
}

func TestDefaultCoverage_NoduplicateRefs(t *testing.T) {
	seen := make(map[string]bool)
	for _, item := range DefaultCoverage() {
		if seen[item.AppcRef] {
			t.Errorf("duplicate AppcRef %q in DefaultCoverage()", item.AppcRef)
		}
		seen[item.AppcRef] = true
	}
}
