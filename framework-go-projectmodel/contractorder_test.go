package projectmodel

import (
	"errors"
	"testing"
)

// Contract order. Construction records the service contracts one component at a time, and the
// record verb refuses a project Load refuses, so Load judges every prefix of the contract set. A
// reference (a contract's component dep, a deployment binding's component or provides) to a
// component the System declares but whose contract is not recorded yet is pending, not an error:
// the order the contracts happen to be recorded in must not decide whether a valid design loads.
// A reference to a component the System does not declare is an error at every prefix, and once
// every contract-bearing component has its contract the full resolution applies.

// orderDoc is a project with a System of the given components (id, name, kind; contractKey is the
// camelCase join), the given contracts, and, when deployment is non-empty, an operationalConcepts
// slot carrying it.
func orderDoc(components, contracts, deployment string) []byte {
	slots := `"5":{"kind":5,"model":{"components":` + components + `,"relationships":[]}}`
	if deployment != "" {
		slots += `,"6":{"kind":6,"model":{"deployment":` + deployment + `}}`
	}
	return []byte(`{"serviceContracts":` + contracts + `,"slots":{` + slots + `}}`)
}

// The System: a Manager that depends on an Engine, and a Resource nobody contracts.
const orderComponents = `[
	{"id":"order-manager","name":"OrderManager","kind":"manager","layer":"manager","contractKey":"orderManager"},
	{"id":"pricing-engine","name":"PricingEngine","kind":"engine","layer":"engine","contractKey":"pricingEngine"},
	{"id":"order-store","name":"OrderStore","kind":"resource","layer":"resource"}
]`

const (
	orderManagerContract  = `"orderManager":{"component":"orderManager","layer":"Manager","goPackage":"internal/manager/order","deps":[{"name":"pricing","component":"pricingEngine"}],"interface":{"name":"OrderManager","operations":[{"name":"Place","params":null,"error":true}]}}`
	pricingEngineContract = `"pricingEngine":{"component":"pricingEngine","layer":"Engine","goPackage":"internal/engine/pricing","interface":{"name":"PricingEngine","operations":[{"name":"Price","params":null,"error":true}]}}`
	orderDeployment       = `{"environments":[{"profile":"local"}],"bindings":[{"component":"pricingEngine","provides":["pricingEngine"],"perProfile":{"local":{"variant":"local","infra":[]}}}]}`
)

func wantProblem(t *testing.T, err error, code string) {
	t.Helper()
	var probs Problems
	if !errors.As(err, &probs) || !probs.Has(code) {
		t.Fatalf("want a %s problem, got %v", code, err)
	}
}

// The Manager's contract is recorded first: its dep on the Engine names a System component whose
// contract is still to come.
func TestLoad_DepOnASystemComponentNotYetContractedIsPending(t *testing.T) {
	if _, err := Load(orderDoc(orderComponents, `{`+orderManagerContract+`}`, "")); err != nil {
		t.Fatalf("Load refuses a dep whose target's contract is not recorded yet: %v", err)
	}
}

// The deployment binds the Engine before any contract is recorded (the operationalConcepts slot is
// a design slot; the contracts come in construction).
func TestLoad_BindingToASystemComponentNotYetContractedIsPending(t *testing.T) {
	if _, err := Load(orderDoc(orderComponents, `{}`, orderDeployment)); err != nil {
		t.Fatalf("Load refuses a binding whose component's contract is not recorded yet: %v", err)
	}
	if _, err := Load(orderDoc(orderComponents, `{`+orderManagerContract+`}`, orderDeployment)); err != nil {
		t.Fatalf("Load refuses a binding whose component's contract is not recorded yet: %v", err)
	}
}

// A dep on a component the System does not declare can never resolve: refused at every prefix.
func TestLoad_DepOnAComponentAbsentFromTheSystemIsRefused(t *testing.T) {
	ghost := `"orderManager":{"component":"orderManager","layer":"Manager","goPackage":"internal/manager/order","deps":[{"name":"ghost","component":"ghostEngine"}],"interface":{"name":"OrderManager","operations":[{"name":"Place","params":null,"error":true}]}}`
	_, err := Load(orderDoc(orderComponents, `{`+ghost+`}`, ""))
	wantProblem(t, err, CodeDepTarget)
}

// A binding to a component the System does not declare is refused at every prefix, for its
// component and for what it provides.
func TestLoad_BindingToAComponentAbsentFromTheSystemIsRefused(t *testing.T) {
	for name, dep := range map[string]string{
		"component": `{"environments":[{"profile":"local"}],"bindings":[{"component":"ghostAccess","perProfile":{"local":{"variant":"local","infra":[]}}}]}`,
		"provides":  `{"environments":[{"profile":"local"}],"bindings":[{"component":"pricingEngine","provides":["ghostAccess"],"perProfile":{"local":{"variant":"local","infra":[]}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(orderDoc(orderComponents, `{}`, dep))
			wantProblem(t, err, CodeDeployRef)
		})
	}
}

// Once every contract-bearing component has its contract, a reference to a System component with
// no contract (here the Resource, which never gets one) is dangling: the full resolution applies.
func TestLoad_OnceEveryContractIsRecordedAnUncontractedReferenceIsRefused(t *testing.T) {
	toStore := `"orderManager":{"component":"orderManager","layer":"Manager","goPackage":"internal/manager/order","deps":[{"name":"pricing","component":"pricingEngine"},{"name":"store","component":"orderStore"}],"interface":{"name":"OrderManager","operations":[{"name":"Place","params":null,"error":true}]}}`
	if _, err := Load(orderDoc(orderComponents, `{`+toStore+`}`, "")); err != nil {
		t.Fatalf("while the Engine's contract is still to come the dep on the Resource is pending: %v", err)
	}
	_, err := Load(orderDoc(orderComponents, `{`+toStore+`,`+pricingEngineContract+`}`, ""))
	wantProblem(t, err, CodeDepTarget)

	bindStore := `{"environments":[{"profile":"local"}],"bindings":[{"component":"orderStore","perProfile":{"local":{"variant":"local","infra":[]}}}]}`
	_, err = Load(orderDoc(orderComponents, `{`+orderManagerContract+`,`+pricingEngineContract+`}`, bindStore))
	wantProblem(t, err, CodeDeployRef)
}

// Without a System nothing is declared, so an unresolved reference is refused as it always was.
func TestLoad_WithoutASystemAnUnresolvedDepIsRefused(t *testing.T) {
	_, err := Load([]byte(`{"serviceContracts":{` + orderManagerContract + `},"slots":{}}`))
	wantProblem(t, err, CodeDepTarget)
}

// The pending dep resolves once its target is recorded, and the layer rule then applies to it.
func TestLoad_APendingDepIsJudgedInFullOnceItsTargetIsRecorded(t *testing.T) {
	if _, err := Load(orderDoc(orderComponents, `{`+orderManagerContract+`,`+pricingEngineContract+`}`, orderDeployment)); err != nil {
		t.Fatalf("the complete design must load: %v", err)
	}
	upward := `"pricingEngine":{"component":"pricingEngine","layer":"Engine","goPackage":"internal/engine/pricing","deps":[{"name":"orders","component":"orderManager"}],"interface":{"name":"PricingEngine","operations":[{"name":"Price","params":null,"error":true}]}}`
	_, err := Load(orderDoc(orderComponents, `{`+orderManagerContract+`,`+upward+`}`, ""))
	wantProblem(t, err, CodeDepTarget)
}
