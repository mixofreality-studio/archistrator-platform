package testgen

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// A planRef is resolved only by the acceptance emitter (FPC plan 4, Tasks 4.3/4.4). A client plan
// never carries one (ATP-REF), so each client-plan emitter refuses one with a generation error that
// names ATP-REF, never dropping the input and never panicking.
func TestPlanRefInput_IsAGenerationErrorInClientPlanEmitters(t *testing.T) {
	in := methodcheck.TestArg{Name: "title", Value: methodcheck.PlanRef{Component: "todoApi", Scenario: "work-the-list-P1", Seq: 1, Input: "title"}}

	op := methodcheck.ContractOperation{Name: "AddTodo", Params: []methodcheck.ContractParam{{Name: "title", Schema: []byte(`{"type":"string"}`)}}}
	if _, _, err := inputsJSON(methodcheck.ServiceContract{}, op, []methodcheck.TestArg{in}); err == nil || !strings.Contains(err.Error(), "ATP-REF") {
		t.Errorf("inputsJSON must refuse a planRef naming ATP-REF, got %v", err)
	}
	if _, err := literalInput(in); err == nil || !strings.Contains(err.Error(), "ATP-REF") {
		t.Errorf("literalInput must refuse a planRef naming ATP-REF, got %v", err)
	}
}
