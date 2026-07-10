package modelgen_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// billingStateAccessRaw extracts the billingStateAccess contract entry (raw
// JSON) from the real archistrator.project.json fixture: it carries a
// uuid.UUID-bound field (Billing.ID) alongside plain scalars and $ref'd
// structs/enums, so it exercises both the UUIDAsString=false (today's
// behavior) and =true paths in one contract.
func billingStateAccessRaw(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var top struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	entry, ok := top.ServiceContracts["billingStateAccess"]
	if !ok {
		t.Fatal("fixture has no billingStateAccess contract")
	}
	return entry
}

// TestEmitTypesUUIDDefault asserts EmitTypes(UUIDAsString: false) — the
// default, matching Generate's own behavior — emits the uuid.UUID type + its
// import for a uuid-bound field, and emits ONLY the types block: no
// "interface" keyword introducing the service-contract interface, no impl
// struct, no DI constructor.
func TestEmitTypesUUIDDefault(t *testing.T) {
	src, err := modelgen.EmitTypes(billingStateAccessRaw(t), modelgen.TypeOptions{PackageName: "billingstate"})
	if err != nil {
		t.Fatalf("EmitTypes: %v", err)
	}
	assertTypesOnly(t, src)

	if !hasField(string(src), "ID", "uuid.UUID", `json:"ID"`) {
		t.Error("expected Billing.ID to be uuid.UUID (UUIDAsString=false)")
	}
	if !strings.Contains(string(src), `"github.com/google/uuid"`) {
		t.Error("expected the uuid import (UUIDAsString=false)")
	}

	checkGolden(t, filepath.Join("../testdata", "archistrator.emittypes.billingstate.gen.go.golden"), src)
}

// TestEmitTypesUUIDAsString asserts EmitTypes(UUIDAsString: true) emits
// `string` in place of uuid.UUID and drops the uuid import entirely — the
// zero-dependency client-mirror mode.
func TestEmitTypesUUIDAsString(t *testing.T) {
	src, err := modelgen.EmitTypes(billingStateAccessRaw(t), modelgen.TypeOptions{
		PackageName:  "billingstate",
		UUIDAsString: true,
	})
	if err != nil {
		t.Fatalf("EmitTypes: %v", err)
	}
	assertTypesOnly(t, src)

	if !hasField(string(src), "ID", "string", `json:"ID"`) {
		t.Error("expected Billing.ID to be string (UUIDAsString=true)")
	}
	if strings.Contains(string(src), "uuid.UUID") {
		t.Error("expected no uuid.UUID reference (UUIDAsString=true)")
	}
	if strings.Contains(string(src), "github.com/google/uuid") {
		t.Error("expected no uuid import (UUIDAsString=true)")
	}

	checkGolden(t, filepath.Join("../testdata", "archistrator.emittypes.billingstate.uuidstring.gen.go.golden"), src)
}

// TestEmitTypesErrorEmptyPackageName asserts EmitTypes rejects an empty
// PackageName naming the field.
func TestEmitTypesErrorEmptyPackageName(t *testing.T) {
	_, err := modelgen.EmitTypes(json.RawMessage(`{"$defs":{"X":{"type":"string"}}}`), modelgen.TypeOptions{})
	if err == nil {
		t.Fatal("EmitTypes should error on empty PackageName")
	}
	if !contains(err.Error(), "PackageName") {
		t.Fatalf("error should name PackageName, got: %v", err)
	}
}

// TestEmitTypesGeneratePrefix asserts EmitTypes' types block is exactly the
// types-block prefix Generate itself emits for the same contract — the two
// share the identical emission path (emitTypesBody), so they can never
// diverge. Uses UUIDAsString=false, Generate's own behavior.
func TestEmitTypesGeneratePrefix(t *testing.T) {
	raw := billingStateAccessRaw(t)

	types, err := modelgen.EmitTypes(raw, modelgen.TypeOptions{PackageName: "billingstate"})
	if err != nil {
		t.Fatalf("EmitTypes: %v", err)
	}

	full, err := modelgen.Generate([]byte(`{"serviceContracts":{"billingStateAccess":`+string(raw)+`}}`), modelgen.Config{
		ModulePath: "example.com/x",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	fullSrc, ok := full["internal/resourceaccess/billingstate"]
	if !ok {
		t.Fatalf("Generate did not return internal/resourceaccess/billingstate (keys: %v)", keysOf(full))
	}

	// Every struct/enum/sum type name EmitTypes emits must also appear, defined
	// identically, in Generate's fuller output (which additionally carries the
	// interface + impl below it).
	for _, defName := range []string{"Billing", "BillingTerms", "CustomerProfile", "Money", "Version"} {
		decl := "type " + defName + " "
		if !strings.Contains(string(types), decl) {
			t.Fatalf("EmitTypes output missing %q", decl)
		}
		if !strings.Contains(string(fullSrc), decl) {
			t.Fatalf("Generate output missing %q", decl)
		}
	}
	if strings.Contains(string(types), "interface {") {
		t.Error("EmitTypes must not emit the service-contract interface")
	}
	if !strings.Contains(string(fullSrc), "interface {") {
		t.Error("sanity: Generate's full output should still emit the interface")
	}
}

// hasField reports whether src contains a struct field line `name type
// `tag“, tolerating gofmt's column-alignment padding (extra spaces) between
// the three tokens.
func hasField(src, name, typ, tag string) bool {
	for _, line := range strings.Split(src, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == name && fields[1] == typ && fields[len(fields)-1] == "`"+tag+"`" {
			return true
		}
	}
	return false
}

// assertTypesOnly asserts src parses as Go and carries none of the
// interface/impl surface Generate emits in addition to types.
func assertTypesOnly(t *testing.T, src []byte) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), "types.go", src, parser.AllErrors); err != nil {
		t.Fatalf("EmitTypes output does not parse: %v\n%s", err, src)
	}
	s := string(src)
	for _, want := range []string{"interface {", "func New", "fwra.Context", "fwm.Context"} {
		if strings.Contains(s, want) {
			t.Errorf("EmitTypes output unexpectedly contains %q (interface/impl leaked into types-only emission)", want)
		}
	}
}
