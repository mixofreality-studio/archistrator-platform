package methodcheck

import (
	"sort"
	"testing"
)

// TestEmittedRuleIDs_ExportsTheRegistrySorted pins the exported view the doctrine registry
// resolves its methodcheck citations against: exactly the emittedRuleIDs set, sorted, unique.
func TestEmittedRuleIDs_ExportsTheRegistrySorted(t *testing.T) {
	got := EmittedRuleIDs()
	set := emittedRuleIDs()
	if len(got) != len(set) {
		t.Fatalf("EmittedRuleIDs has %d ids, the registry %d", len(got), len(set))
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
		t.Errorf("EmittedRuleIDs is not sorted")
	}
	for i, id := range got {
		if !set[id] {
			t.Errorf("EmittedRuleIDs lists %q, which the registry does not", id)
		}
		if i > 0 && got[i-1] == id {
			t.Errorf("EmittedRuleIDs lists %q twice", id)
		}
	}
	for _, want := range []RuleID{"UID-ROLE", "UID-NAME", "TP-HOOK", "TP-UI-TARGET", "TP-CLIENT-CONTRACTED", "SYS-SURFACE-ENTRY", "SYS-SURFACE-REQUIRED"} {
		if !set[want] {
			t.Errorf("registry lacks %q, which the doctrine registry cites", want)
		}
	}
}
