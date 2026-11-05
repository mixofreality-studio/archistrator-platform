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

func TestDefaultDeploymentIsDerivedFromTheContracts(t *testing.T) {
	contracts := map[string]*Contract{
		"list":   {Key: "list", Layer: "Manager", GoPackage: "internal/manager/list"},
		"todo":   {Key: "todo", Layer: "ResourceAccess", GoPackage: "internal/resourceaccess/todo", Infra: []string{"Postgres"}},
		"caller": {Key: "caller", Layer: "ResourceAccess", GoPackage: "internal/resourceaccess/caller", Infra: []string{"ExternalHTTP"}},
		"web":    {Key: "web", Layer: "Client"},
	}
	d := DefaultDeployment(contracts, "server")
	if len(d.Containers) != 1 || d.Containers[0].Key != "server" || len(d.Containers[0].Components) != 3 {
		t.Fatalf("containers = %+v", d.Containers)
	}
	infra := map[string]bool{}
	for _, i := range d.Infrastructure {
		infra[i.Key] = true
	}
	if !infra["temporal"] || !infra["postgres"] {
		t.Errorf("a Manager needs Temporal and a Postgres contract needs a pool: %+v", d.Infrastructure)
	}
	if len(d.Bindings) != 2 {
		t.Errorf("bindings = %+v", d.Bindings)
	}
}

func TestEffectiveDeploymentPrefersTheAuthoredOne(t *testing.T) {
	authored := &Deployment{Containers: []DeployContainer{{Key: "authored"}}}
	if got := (&Model{Deployment: authored}).EffectiveDeployment("server"); got != authored {
		t.Errorf("an authored deployment must win, got %+v", got)
	}
	if got := (&Model{}).EffectiveDeployment("server"); got.Containers[0].Key != "server" {
		t.Errorf("no authored deployment must fall back to the default, got %+v", got)
	}
}

// An ExternalHTTP binding's endpoint is declared configuration (FIX-049): one external-http
// input per binding, keyed by the component, required in each profile that binds it, and the
// binding consumes it. A hand hook then reads the URL from the generated Config instead of an
// env var of its own invention.
func TestDefaultDeploymentDeclaresEachExternalEndpoint(t *testing.T) {
	contracts := map[string]*Contract{
		"list":           {Key: "list", Layer: "Manager", GoPackage: "internal/manager/list"},
		"identityAccess": {Key: "identityAccess", Layer: "ResourceAccess", GoPackage: "internal/resourceaccess/identity", Infra: []string{"ExternalHTTP"}},
	}
	d := DefaultDeployment(contracts, "server")
	var decl *InfraDecl
	for i := range d.Infrastructure {
		if d.Infrastructure[i].Substrate == SubstrateExternalHTTP {
			decl = &d.Infrastructure[i]
		}
	}
	if decl == nil || decl.Key != "identity-access" || decl.Presence != "required" || strings.Join(decl.Profiles, ",") != "cloud,local" {
		t.Fatalf("external endpoint decl = %+v in %+v", decl, d.Infrastructure)
	}
	for profile, v := range d.Bindings[0].PerProfile {
		if len(v.Infra) != 1 || v.Infra[0] != "identity-access" {
			t.Errorf("profile %s: the ExternalHTTP binding must consume its endpoint, got %+v", profile, v)
		}
	}
	if err := (&Model{Contracts: contracts, Deployment: d}).validateDeployment(); err != nil {
		t.Errorf("the default deployment must validate: %v", err)
	}
}

// A System with a built browser surface gets the webDistDir setting in its effective default
// deployment (FIX-051), and one without gets none.
func TestEffectiveDeploymentDeclaresWebDistDirForABrowserSurface(t *testing.T) {
	yes := true
	withSurface := &Model{System: &System{Components: []SystemComponent{{ID: "web", Kind: "client", UISurface: &yes}}}}
	if !hasSetting(withSurface.EffectiveDeployment("server"), WebDistDirSetting) {
		t.Error("a browser surface must declare webDistDir")
	}
	planned := &Model{System: &System{Components: []SystemComponent{{ID: "web", Kind: "client", UISurface: &yes, BuildStatus: "planned"}}}}
	if hasSetting(planned.EffectiveDeployment("server"), WebDistDirSetting) || hasSetting((&Model{}).EffectiveDeployment("server"), WebDistDirSetting) {
		t.Error("no built browser surface must declare no webDistDir")
	}
}

func hasSetting(d *Deployment, name string) bool {
	for _, s := range d.Settings {
		if s.Name == name {
			return true
		}
	}
	return false
}
