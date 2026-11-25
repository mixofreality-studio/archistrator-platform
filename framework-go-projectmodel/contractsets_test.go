package projectmodel

import "testing"

// The two contract sets are one definition: the kinds that author a contract are the kinds that
// bear one, less the Client, whose contract is derived. A Resource bears none.
func TestKindContractSets(t *testing.T) {
	for _, tc := range []struct {
		kind          string
		bears, writes bool
	}{
		{"client", true, false}, {"Client", true, false}, {"manager", true, true}, {"engine", true, true},
		{"resourceAccess", true, true}, {"Resource Access", true, true}, {"utility", true, true},
		{"resource", false, false}, {"", false, false},
	} {
		if got := KindBearsContract(tc.kind); got != tc.bears {
			t.Errorf("KindBearsContract(%q) = %v, want %v", tc.kind, got, tc.bears)
		}
		if got := KindAuthorsContract(tc.kind); got != tc.writes {
			t.Errorf("KindAuthorsContract(%q) = %v, want %v", tc.kind, got, tc.writes)
		}
	}
	planned := SystemComponent{Kind: "manager", BuildStatus: "planned"}
	if planned.OwesContract() || planned.OwesAuthoredContract() {
		t.Error("a planned component owes no contract")
	}
	if c := (SystemComponent{Kind: "client"}); !c.OwesContract() || c.OwesAuthoredContract() {
		t.Error("a built Client owes a contract it does not author")
	}
}
