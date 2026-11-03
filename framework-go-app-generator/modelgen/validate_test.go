package modelgen_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

func TestValidateContract(t *testing.T) {
	for _, tc := range []struct {
		name, doc, want string
	}{
		{"an RA with an approved infra", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"interface":{"name":"TodoAccess","layer":"resourceaccess"}}`, ""},
		{"an external service", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/caller","infra":["ExternalHTTP"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, ""},
		{"a manager", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/manager/list","interface":{"name":"ListManager","layer":"manager"}}`, ""},
		{"a client with no package", `{"interface":{"name":"Web","layer":"client"}}`, ""},
		{"a capitalised layer", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/todo","infra":["Postgres"],"interface":{"name":"TodoAccess","layer":"ResourceAccess"}}`, "interface.layer"},
		{"an unapproved infra names the menu", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/caller","infra":["IdentityProvider"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "ExternalHTTP"},
		{"an RA with no infra", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/caller","interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "must bind"},
		{"a package outside its layer", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/domain/caller","infra":["Postgres"],"interface":{"name":"CallerAccess","layer":"resourceaccess"}}`, "internal/resourceaccess/"},
		{"an engine op returning an error", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do","error":true}]}}`, ""},
		{"a total engine op with no error", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do","error":false}]}}`, "operation Do must declare error:true"},
		{"an engine op omitting error", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/engine/a","interface":{"name":"AEngine","layer":"engine","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"an RA op with no error", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/a","infra":["Postgres"],"interface":{"name":"AAccess","layer":"resourceaccess","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"a manager op with no error", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/manager/a","interface":{"name":"AManager","layer":"manager","operations":[{"name":"Do"}]}}`, "operation Do must declare error:true"},
		{"an engine named without its suffix", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/engine/a","interface":{"name":"Resolver","layer":"engine"}}`, "must end in Engine"},
		{"an RA named without its suffix", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/resourceaccess/a","infra":["Postgres"],"interface":{"name":"Store","layer":"resourceaccess"}}`, "must end in Access"},
		{"a utility op may be total", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/utility/a","interface":{"name":"Clock","layer":"utility","operations":[{"name":"Now"}]}}`, ""},
		{"a lowercase utility op, RUN-009's wall", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/utility/a","interface":{"name":"PubSub","layer":"utility","operations":[{"name":"publish"}]}}`, "exported Go identifier"},
		{"the refusal says a view's label still resolves", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/utility/a","interface":{"name":"PubSub","layer":"utility","operations":[{"name":"publish"}]}}`, "ignoring case"},
		{"a lowercase interface", `{"$defs":{"T":{"type":"string"}},"goPackage":"internal/utility/a","interface":{"name":"pubSub","layer":"utility"}}`, "interface.name"},
		{"no interface name", `{"interface":{"layer":"manager"}}`, "interface.name"},
		{"a keyword param", `{"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","params":[{"name":"type"}]}]}}`, "not a keyword"},
		{"the call Context's name", `{"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","params":[{"name":"rc"}]}]}}`, "collides"},
		{"a param spelled with a dash", `{"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","params":[{"name":"a-b"}]}]}}`, "Go identifier"},
		{"two params folding together", `{"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","params":[{"name":"id"},{"name":"ID"}]}]}}`, "differ at most in case"},
		{"two ops folding together", `{"interface":{"name":"M","layer":"utility","operations":[{"name":"DoIt"},{"name":"Doit"}]}}`, "differ at most in case"},
		{"a built contract with no types", `{"goPackage":"internal/manager/a","interface":{"name":"AManager","layer":"manager"}}`, "$defs is empty"},
		{"a type named like the interface", `{"$defs":{"M":{"type":"string"}},"interface":{"name":"M","layer":"utility"}}`, "$defs name"},
		{"a type that is no identifier", `{"$defs":{"a-b":{"type":"string"}},"interface":{"name":"M","layer":"utility"}}`, "$defs name"},
		{"a property that is no identifier", `{"$defs":{"T":{"type":"object","properties":{"due-date":{"type":"string"}}}},"interface":{"name":"M","layer":"utility"}}`, "due-date"},
		{"two properties one field", `{"$defs":{"T":{"type":"object","properties":{"id":{"type":"string"},"Id":{"type":"string"}}}},"interface":{"name":"M","layer":"utility"}}`, "same Go field"},
		{"an enum constant that is no identifier", `{"$defs":{"T":{"type":"string","enum":["a"],"x-enum-varnames":["a-b"]}},"interface":{"name":"M","layer":"utility"}}`, "x-enum-varnames"},
		{"a reference to nothing", `{"$defs":{"T":{"type":"string"}},"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","result":{"$ref":"#/$defs/U"}}]}}`, "names no type"},
		{"an unexported type on an op", `{"$defs":{"t":{"type":"string"}},"interface":{"name":"M","layer":"utility","operations":[{"name":"Do","result":{"$ref":"#/$defs/t"}}]}}`, "unexported"},
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

// The fresh-project fixture every generator golden is built over stays legal under the rule.
func TestValidateContractAcceptsTheGreenfieldProject(t *testing.T) {
	raw, err := os.ReadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for key, contract := range doc.ServiceContracts {
		if err := modelgen.ValidateContract(key, contract); err != nil {
			t.Error(err)
		}
	}
}

// A Manager's params become its machine clients' params, and one param named action is the shape
// of a browser operation, which would misroute those clients' tests and stop generation.
func TestValidateContractRefusesAManagerActionOp(t *testing.T) {
	contract := func(layer, name, params string) []byte {
		return []byte(`{"component":"c","layer":"` + layer + `","title":"t","interface":{"name":"` + name + `","layer":"` + layer + `","operations":[{"name":"Apply","params":[` + params + `],"error":true}]}}`)
	}
	action := `{"name":"action","schema":{"type":"string"}}`
	if err := modelgen.ValidateContract("listManager", contract("manager", "ListManager", action)); err == nil || !strings.Contains(err.Error(), "browser action") {
		t.Errorf("a Manager op whose one param is action: %v", err)
	}
	if err := modelgen.ValidateContract("listManager", contract("manager", "ListManager", action+`,{"name":"proof","schema":{"type":"string"}}`)); err != nil {
		t.Errorf("action beside another param is no browser op: %v", err)
	}
	if err := modelgen.ValidateContract("rulesEngine", contract("engine", "RulesEngine", action)); err != nil {
		t.Errorf("only a Manager's params reach a machine client: %v", err)
	}
}
