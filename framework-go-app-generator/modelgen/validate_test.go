package modelgen_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
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
