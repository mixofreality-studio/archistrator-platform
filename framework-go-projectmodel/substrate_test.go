package projectmodel

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// A deployment's infrastructure names a substrate from the catalog; any other is a problem the
// model reports at load, not a generator's late "unknown substrate" error.
func TestValidate_UnknownSubstrate(t *testing.T) {
	dep := `{"environments":[{"profile":"local"}],
		"infrastructure":[{"key":"cache","substrate":"memcached","profiles":["local"],"presence":"required"}]}`
	_, err := Load(deployDoc("", dep))
	var probs Problems
	if !errors.As(err, &probs) || !probs.Has(CodeSubstrateUnknown) {
		t.Fatalf("want %s, got %v", CodeSubstrateUnknown, err)
	}
	p := findProblem(t, probs, CodeSubstrateUnknown)
	if want := "deployment.infrastructure[0].substrate"; p.Path != want {
		t.Fatalf("path: got %q, want %q", p.Path, want)
	}
	if got := ValidateDeployment(json.RawMessage(dep), nil); !got.Has(CodeSubstrateUnknown) {
		t.Fatalf("ValidateDeployment: want %s, got %v", CodeSubstrateUnknown, got)
	}
	ok := `{"environments":[{"profile":"local"}],
		"infrastructure":[{"key":"repo","substrate":"git-repo","profiles":["local"],"presence":"required"}]}`
	if _, err := Load(deployDoc("", ok)); err != nil {
		t.Fatalf("git-repo is a catalog substrate: %v", err)
	}
}

// A dependency's Setting links it to a deployment setting of a compatible type, so the same value
// reaches the Manager in production (cfg.<Field>) and in a scenario (the setting's default).
func TestValidate_DepSettingResolves(t *testing.T) {
	const dep = `{"environments":[{"profile":"local"}],"settings":[
		{"name":"escalationTimeout","type":"duration","default":"1h"},
		{"name":"mode","type":"string","default":"x"}]}`
	manager := func(depJSON string) string {
		return `{"deliveryManager":{"component":"deliveryManager","layer":"Manager","goPackage":"internal/manager/delivery","$defs":{},
			"deps":[` + depJSON + `],
			"interface":{"name":"DeliveryManager","layer":"manager","operations":[{"name":"Ping","params":null,"error":false}]}}}`
	}
	cases := []struct {
		name, dep string
		bad       bool
	}{
		{"duration setting for a time.Duration dep", `{"name":"wait","goType":"time.Duration","goImport":"time","setting":"escalationTimeout"}`, false},
		{"string setting for a string dep", `{"name":"mode","goType":"string","setting":"mode"}`, false},
		{"unknown setting", `{"name":"wait","goType":"time.Duration","goImport":"time","setting":"ghost"}`, true},
		{"string setting for a time.Duration dep", `{"name":"wait","goType":"time.Duration","goImport":"time","setting":"mode"}`, true},
		{"setting on a component dep", `{"name":"wait","component":"deliveryManager","setting":"mode"}`, true},
	}
	for _, c := range cases {
		_, err := Load(deployDoc(manager(c.dep), dep))
		var probs Problems
		got := errors.As(err, &probs) && probs.Has(CodeDepSetting)
		if got != c.bad {
			t.Errorf("%s: want problem=%v, got %v", c.name, c.bad, err)
		}
		if err != nil && !c.bad {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
	}
}

func TestSubstrates_EveryKindHasInputs(t *testing.T) {
	want := []Substrate{SubstrateTemporal, SubstratePostgres, SubstrateGitHubApp, SubstrateKeycloak, SubstrateOtel, SubstrateExternalHTTP, SubstrateGitRepo}
	if !slices.Equal(Substrates(), want) {
		t.Fatalf("Substrates() = %v, want %v", Substrates(), want)
	}
	for _, s := range Substrates() {
		in := s.Inputs()
		if s == SubstrateOtel {
			if len(in) != 0 {
				t.Errorf("otel is env-driven by the OTEL_* convention and declares no inputs, got %v", in)
			}
			continue
		}
		if len(in) == 0 {
			t.Errorf("substrate %q declares no inputs", s)
		}
	}
	if got := SubstrateGitRepo.Inputs(); len(got) != 1 || got[0].Name != "URL" || got[0].Default != "" {
		t.Errorf("git-repo has one required URL input, got %v", got)
	}
	if got := SubstrateTemporal.Inputs(); got[0].Default == "" {
		t.Errorf("temporal HOSTPORT keeps its dev-cluster default, got %v", got)
	}
	if Substrate("memcached").Inputs() != nil {
		t.Error("an unknown substrate has no inputs")
	}
}

func TestInfraNaming(t *testing.T) {
	if got := InfraFieldName("project-state-git-repo", "URL"); got != "ProjectStateGitRepoURL" {
		t.Errorf("InfraFieldName = %q", got)
	}
	if got := InfraFieldName("temporal", "HOSTPORT"); got != "TemporalHostport" {
		t.Errorf("InfraFieldName = %q", got)
	}
	d := InfraDecl{Key: "project-state-git-repo", Substrate: SubstrateGitRepo}
	if got := InfraEnvName("ARCHISTRATOR", d, "URL"); got != "ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL" {
		t.Errorf("InfraEnvName = %q", got)
	}
	d.Env = map[string]string{"URL": "CUSTOM_URL"}
	if got := InfraEnvName("ARCHISTRATOR", d, "URL"); got != "CUSTOM_URL" {
		t.Errorf("InfraEnvName override = %q", got)
	}
	if LocalProfile != "local" {
		t.Errorf("LocalProfile = %q", LocalProfile)
	}
}
