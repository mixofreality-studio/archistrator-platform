// deployment_test.go
package projectmodel

import (
	"strings"
	"testing"
)

// deployDoc wraps a deployment JSON fragment in a minimal project document
// carrying a single operationalConcepts slot (kind==6). contracts is an
// optional serviceContracts fragment ("" ⇒ none).
func deployDoc(contracts, deployment string) []byte {
	sc := ""
	if contracts != "" {
		sc = `"serviceContracts":` + contracts + `,`
	}
	return []byte(`{` + sc +
		`"slots":{"6":{"kind":6,"model":{"deployment":` + deployment + `}}}}`)
}

// fooAccessContract is a minimal valid ResourceAccess contract, keyed
// "fooAccess", used as a resolvable binding target.
const fooAccessContract = `{"fooAccess":{"component":"fooAccess","layer":"ResourceAccess","interface":{"name":"FooAccess"}}}`

// TestLoadArchistratorFixtureEmptyDeploymentExtras pins that the archistrator
// fixture — which carries the legacy deployment (deliveryStyle/containers/
// environments) but NONE of the appgen sections — parses into a Deployment
// whose appgen slices are empty and validates clean.
func TestLoadArchistratorFixtureEmptyDeploymentExtras(t *testing.T) {
	m, err := LoadFile("testdata/archistrator.project.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.Deployment == nil {
		t.Fatal("deployment not parsed")
	}
	if m.Deployment.DeliveryStyle != "cloud" {
		t.Fatalf("deliveryStyle: %q", m.Deployment.DeliveryStyle)
	}
	if len(m.Deployment.Environments) == 0 {
		t.Fatal("environments not parsed")
	}
	if len(m.Deployment.Infrastructure) != 0 || len(m.Deployment.Bindings) != 0 || len(m.Deployment.Settings) != 0 {
		t.Fatalf("appgen sections should be empty: infra=%d bindings=%d settings=%d",
			len(m.Deployment.Infrastructure), len(m.Deployment.Bindings), len(m.Deployment.Settings))
	}
}

// TestLoadDeploymentPresentValidates exercises a fully-populated deployment
// (2 profiles, required + optional-dormant infra, env override, a binding with
// perProfile variants + provides, all four setting types) and asserts it
// parses and validates clean.
func TestLoadDeploymentPresentValidates(t *testing.T) {
	dep := `{
		"deliveryStyle":"cloud",
		"environments":[{"profile":"cloud"},{"profile":"local"}],
		"infrastructure":[
			{"key":"temporal","substrate":"temporal","profiles":["cloud","local"],"presence":"required","env":{"HOSTPORT":"ARCHISTRATOR_TEMPORAL_HOSTPORT"}},
			{"key":"github-app","substrate":"github-app","profiles":["cloud"],"presence":"optional-dormant"}
		],
		"bindings":[
			{"component":"fooAccess","presence":"optional","provides":["fooAccess"],
			 "perProfile":{"cloud":{"variant":"github","infra":["github-app","temporal"]},"local":{"variant":"local","infra":["temporal"]}}}
		],
		"settings":[
			{"name":"listenAddr","type":"string","default":":8080"},
			{"name":"authDevMode","type":"bool","default":"false"},
			{"name":"installationID","type":"int","default":"0"},
			{"name":"shutdownTimeout","type":"duration","default":"20s"}
		]
	}`
	m, err := Load(deployDoc(fooAccessContract, dep))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Deployment.Infrastructure) != 2 {
		t.Fatalf("infra: %d", len(m.Deployment.Infrastructure))
	}
	if len(m.Deployment.Bindings) != 1 || len(m.Deployment.Settings) != 4 {
		t.Fatalf("bindings=%d settings=%d", len(m.Deployment.Bindings), len(m.Deployment.Settings))
	}
}

func TestLoadRejectsBadPresence(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"infrastructure":[{"key":"temporal","substrate":"temporal","profiles":["cloud"],"presence":"maybe"}]}`
	_, err := Load(deployDoc("", dep))
	if err == nil || !strings.Contains(err.Error(), "unknown presence") {
		t.Fatalf("want presence error, got %v", err)
	}
}

func TestLoadRejectsInfraProfileNotInEnvironments(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"infrastructure":[{"key":"temporal","substrate":"temporal","profiles":["staging"],"presence":"required"}]}`
	_, err := Load(deployDoc("", dep))
	if err == nil || !strings.Contains(err.Error(), "not in environments") {
		t.Fatalf("want profile error, got %v", err)
	}
}

func TestLoadRejectsBindingUnknownComponent(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"bindings":[{"component":"ghostAccess","perProfile":{"cloud":{"variant":"x"}}}]}`
	_, err := Load(deployDoc("", dep))
	if err == nil || !strings.Contains(err.Error(), "unknown component") {
		t.Fatalf("want unknown-component error, got %v", err)
	}
}

func TestLoadRejectsBindingDanglingInfra(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"infrastructure":[{"key":"temporal","substrate":"temporal","profiles":["cloud"],"presence":"required"}],
		"bindings":[{"component":"fooAccess","perProfile":{"cloud":{"variant":"x","infra":["postgres"]}}}]}`
	_, err := Load(deployDoc(fooAccessContract, dep))
	if err == nil || !strings.Contains(err.Error(), "unknown infra key") {
		t.Fatalf("want dangling-infra error, got %v", err)
	}
}

func TestLoadRejectsBindingProfileNotInEnvironments(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"bindings":[{"component":"fooAccess","perProfile":{"staging":{"variant":"x"}}}]}`
	_, err := Load(deployDoc(fooAccessContract, dep))
	if err == nil || !strings.Contains(err.Error(), "not in environments") {
		t.Fatalf("want binding-profile error, got %v", err)
	}
}

func TestLoadRejectsBindingDanglingProvides(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"bindings":[{"component":"fooAccess","provides":["ghostAccess"],"perProfile":{"cloud":{"variant":"x"}}}]}`
	_, err := Load(deployDoc(fooAccessContract, dep))
	if err == nil || !strings.Contains(err.Error(), "provides unknown component") {
		t.Fatalf("want dangling-provides error, got %v", err)
	}
}

func TestLoadRejectsBadSettingType(t *testing.T) {
	dep := `{"environments":[{"profile":"cloud"}],
		"settings":[{"name":"listenAddr","type":"float","default":"0"}]}`
	_, err := Load(deployDoc("", dep))
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("want setting-type error, got %v", err)
	}
}
