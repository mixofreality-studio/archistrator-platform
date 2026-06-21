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
		if item.Classification != AppCHumanJudgment && item.RuleID == "" {
			t.Errorf("appcRef=%q is non-human-judgment but has no RuleID", item.AppcRef)
		}
		if item.Kind != AppCDirective && item.Kind != AppCGuideline {
			t.Errorf("appcRef=%q has invalid Kind=%q", item.AppcRef, item.Kind)
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
