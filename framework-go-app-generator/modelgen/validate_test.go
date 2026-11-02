package modelgen_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

func TestValidateContract(t *testing.T) {
	for _, tc := range []struct {
		name, doc, want string
	}{
		{"an RA with an approved infra", `{"goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"interface":{"name":"TodoAccess","layer":"resourceaccess"}}`, ""},
		{"an external service", `{"goPackage":"internal/resourceaccess/caller","infra":["ExternalHTTP"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, ""},
		{"a manager", `{"goPackage":"internal/manager/list","interface":{"name":"ListManager","layer":"manager"}}`, ""},
		{"a client with no package", `{"interface":{"name":"Web","layer":"client"}}`, ""},
		{"a capitalised layer", `{"goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"interface":{"name":"TodoAccess","layer":"ResourceAccess"}}`, "interface.layer"},
		{"an unapproved infra names the menu", `{"goPackage":"internal/resourceaccess/caller","infra":["IdentityProvider"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "ExternalHTTP"},
		{"an RA with no infra", `{"goPackage":"internal/resourceaccess/caller","interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "must bind"},
		{"a package outside its layer", `{"goPackage":"internal/domain/caller","infra":["Postgres"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "internal/resourceaccess/"},
		{"an engine op returning an error", `{"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do","error":true}]}}`, ""},
		{"a total engine op with no error", `{"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do","error":false}]}}`, "operation Do must declare error:true"},
		{"an engine op omitting error", `{"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"an RA op with no error", `{"goPackage":"internal/resourceaccess/a","infra":["Postgres"],"interface":{"name":"AAccess","layer":"resourceaccess","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"a manager op with no error", `{"goPackage":"internal/manager/a","interface":{"name":"AManager","layer":"manager","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"an engine named without its suffix", `{"goPackage":"internal/engine/a","interface":{"name":"Resolver","layer":"engine"}}`, "must end in Engine"},
		{"an RA named without its suffix", `{"goPackage":"internal/resourceaccess/a","infra":["Postgres"],"interface":{"name":"Store","layer":"resourceaccess"}}`, "must end in Access"},
		{"a utility op may be total", `{"goPackage":"internal/utility/a","interface":{"name":"Clock","layer":"utility","operations":[{"name":"Now"}]}}`, ""},
		{"a client op may be total", `{"interface":{"name":"Web","layer":"client","operations":[{"name":"Open"}]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := modelgen.ValidateContract("k", []byte(tc.doc))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %q", err, tc.want)
			}
		})
	}
}

// The contract's layer rules and the gate that refuses their generated code must not
// drift: a layer the gate holds to an interface suffix (and so to the error-last rule,
// arch.go checkInterfaceMethodReturns) must be refused at record time, under exactly the
// suffix the gate matches, and a layer the gate leaves free stays free of the suffix rule.
func TestContractLayerRulesMatchArch(t *testing.T) {
	contract := func(layer, name, op string) string {
		return `{"infra":["Postgres"],"interface":{"name":"` + name + `","layer":"` + layer + `","operations":[` + op + `]}}`
	}
	validate := func(layer, name, op string) error {
		return modelgen.ValidateContract("k", []byte(contract(layer, name, op)))
	}
	const total, returnsError = `{"name":"Do"}`, `{"name":"Do","error":true}`
	layers := arch.MethodSpec(t.TempDir()).Layers
	if len(layers) == 0 {
		t.Fatal("arch.MethodSpec has no layers")
	}
	for _, l := range layers {
		wire := strings.ToLower(l.Name)
		t.Run(wire, func(t *testing.T) {
			if l.IfaceSuffix == nil {
				if err := validate(wire, "AnyName", returnsError); err != nil {
					t.Errorf("the gate has no name rule for %s, so the contract must have none: %v", wire, err)
				}
				return
			}
			named := "A" + strings.TrimSuffix(l.IfaceSuffix.String(), "$")
			if !l.IfaceSuffix.MatchString(named) {
				t.Fatalf("test could not spell a name matching %s", l.IfaceSuffix)
			}
			if err := validate(wire, named, returnsError); err != nil {
				t.Errorf("a name and op the gate accepts was refused: %v", err)
			}
			if err := validate(wire, "Other", returnsError); err == nil {
				t.Errorf("the gate requires %s, the contract must refuse a name without it", l.IfaceSuffix)
			}
			if err := validate(wire, named, total); err == nil {
				t.Errorf("the gate requires error last on %s, the contract must refuse an op without it", wire)
			}
		})
	}
	if err := validate("utility", "Clock", total); err != nil {
		t.Errorf("the gate has no return rule on a Utility: %v", err)
	}
}

const generateDoc = `{"serviceContracts":{
"a":{"component":"a","goPackage":"internal/engine/a","$defs":{"A":{"type":"object"}},"interface":{"name":"AEngine","layer":"engine","operations":[]}},
"b":{"component":"b","goPackage":"internal/manager/b","$defs":{"B":{"type":"object"}},"interface":{"name":"BManager","layer":"manager","operations":[]}},
"bad":{"component":"bad","goPackage":"internal/resourceaccess/bad","infra":["IdentityProvider"],"$defs":{"C":{"type":"object"}},"interface":{"name":"BadAccess","layer":"resourceaccess","operations":[]}},
"shape":{"component":"shape","goPackage":"internal/resourceaccess/shape","$defs":{"D":{"type":"object"}},"interface":{"name":"ShapeAccess","layer":"ResourceAccess","operations":[]}},
"web":{"component":"web","layer":"Client","goPackage":"internal/client/mcp/x","$defs":{"E":{"type":"object"}},"interface":{"name":"Web","layer":"client","operations":[]}}
}}`

// A task generates its own contracts: a sibling's bad infra cannot abort it, and every
// engine or manager gets a delegating constructor with no allowlist to get onto.
func TestGenerateOnlyNamedContracts(t *testing.T) {
	got, err := modelgen.Generate([]byte(generateDoc), modelgen.Config{ModulePath: "example.com/m", Only: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d packages, want the 2 named", len(got))
	}
	if src := string(got["internal/engine/a"]); !strings.Contains(src, "func NewAEngine() AEngine") || !strings.Contains(src, "return newAEngine()") {
		t.Errorf("an engine must get a zero-param delegating constructor:\n%s", src)
	}
	if src := string(got["internal/manager/b"]); !strings.Contains(src, "func NewBManager() BManager") || !strings.Contains(src, "return newBManager()") {
		t.Errorf("a manager with no deps must get a zero-param delegating constructor:\n%s", src)
	}
}

func TestGenerateRefusesWhatItCannotEmit(t *testing.T) {
	cfg := func(only ...string) modelgen.Config {
		return modelgen.Config{ModulePath: "example.com/m", Only: only}
	}
	if _, err := modelgen.Generate([]byte(generateDoc), cfg("shape")); err == nil || !strings.Contains(err.Error(), "unknown layer") {
		t.Errorf("a capitalised layer must be a hard error, got %v", err)
	}
	if _, err := modelgen.Generate([]byte(generateDoc), cfg("nope")); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("an unknown key must be named, got %v", err)
	}
	if _, err := modelgen.Generate([]byte(generateDoc), cfg("web")); err == nil {
		t.Errorf("a client contract has no generatable entry")
	}
}

func TestExternalHTTPConstructor(t *testing.T) {
	doc := `{"serviceContracts":{"caller":{"component":"caller","goPackage":"internal/resourceaccess/caller","infra":["ExternalHTTP"],"$defs":{"C":{"type":"object"}},"interface":{"name":"CallerAccess","layer":"resourceaccess","operations":[]}}}}`
	got, err := modelgen.Generate([]byte(doc), modelgen.Config{ModulePath: "example.com/m"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(got["internal/resourceaccess/caller"])
	if !strings.Contains(src, "func NewExternalHTTPCallerAccess(baseURL string, client *http.Client) (CallerAccess, error)") {
		t.Errorf("no ExternalHTTP constructor:\n%s", src)
	}
}
