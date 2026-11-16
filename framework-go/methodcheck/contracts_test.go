package methodcheck

import (
	"slices"
	"testing"
)

func TestContractsOf_KeyAndFacets(t *testing.T) {
	comps := map[string]string{"projectStateAccess": "", "activityExecutionAccess": "projectStateAccess", "listManager": ""}
	if got := ContractsOf("projectStateAccess", comps); !slices.Equal(got, []string{"activityExecutionAccess", "projectStateAccess"}) {
		t.Fatalf("ContractsOf = %v", got)
	}
	if got := ContractsOf("ghost", comps); !slices.Equal(got, []string{"ghost"}) { // a named key with no contract is still named
		t.Fatalf("unrecorded key = %v", got)
	}
	if ContractsOf("", comps) != nil {
		t.Fatal("empty key must name nothing")
	}
}

func TestContractsOf_ExactMatchOnly(t *testing.T) {
	if got := ContractsOf("listManager", map[string]string{"listManager": "", "x": "list-manager"}); !slices.Equal(got, []string{"listManager"}) {
		t.Fatalf("a normalized spelling must not join: %v", got)
	}
}
