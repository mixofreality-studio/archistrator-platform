package modelgen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

const sumTypeModule = "example.com/sumtypeproof"

// sumTypeFixture is the minimal Shape contract: a two-variant sum over A and B.
func sumTypeFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "sumtype", "contract.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// generateSumType runs the whole generator over the fixture, as a project's one contract.
func generateSumType(t *testing.T) string {
	t.Helper()
	project := []byte(`{"serviceContracts":{"shapeEngine":` + string(sumTypeFixture(t)) + `}}`)
	out, err := modelgen.Generate(project, modelgen.Config{ModulePath: sumTypeModule})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	src, ok := out["internal/engine/shape"]
	if !ok {
		t.Fatalf("no output for internal/engine/shape; got %d packages", len(out))
	}
	return string(src)
}

// sumTypeModuleDir writes the fixture's types block as a stdlib-only package in a fresh
// module, plus a main package running prog against it, and returns the module directory.
func sumTypeModuleDir(t *testing.T, prog string) string {
	t.Helper()
	types, err := modelgen.EmitTypes(sumTypeFixture(t), modelgen.TypeOptions{PackageName: "shape"})
	if err != nil {
		t.Fatalf("EmitTypes: %v", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":             "module " + sumTypeModule + "\n\ngo 1.25\n",
		"shape/shape.gen.go": string(types),
		"cmd/proof/main.go":  prog,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// goIn runs the go tool in dir, isolated from any enclosing workspace.
func goIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestSumType_EmitsSealedInterface(t *testing.T) {
	src := generateSumType(t)
	for _, want := range []string{
		"//sumtype:decl",
		"type Shape interface",
		"Kind() ShapeKind",
		"isShape()",
		"func (*A) isShape()",
		"func (*B) isShape()",
		"func (*A) Kind() ShapeKind",
		"func (*B) Kind() ShapeKind",
		"func UnmarshalShape(",
		"func MarshalShape(",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source lacks %q", want)
		}
	}
}

const unknownKindProgram = `package main

import (
	"fmt"
	"os"

	"` + sumTypeModule + `/shape"
)

func main() {
	_, err := shape.UnmarshalShape([]byte("{\"kind\":\"c\",\"model\":{}}"))
	if err == nil {
		fmt.Println("NO ERROR")
		os.Exit(1)
	}
	fmt.Println(err.Error())
}
`

func TestSumType_UnmarshalRefusesUnknownKind(t *testing.T) {
	dir := sumTypeModuleDir(t, unknownKindProgram)
	goIn(t, dir, "vet", "./...")
	out := goIn(t, dir, "run", "./cmd/proof")
	if !strings.Contains(out, `unknown kind "c"`) {
		t.Errorf("error does not name the unknown kind; got %q", out)
	}
}

const roundTripProgram = `package main

import (
	"fmt"
	"os"

	"` + sumTypeModule + `/shape"
)

func main() {
	b, err := shape.MarshalShape(&shape.A{Side: 2})
	if err != nil {
		fmt.Println("marshal:", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
	v, err := shape.UnmarshalShape(b)
	if err != nil {
		fmt.Println("unmarshal:", err)
		os.Exit(1)
	}
	a, ok := v.(*shape.A)
	if !ok || a.Side != 2 || a.Kind() != shape.ShapeKindA {
		fmt.Printf("round trip lost the value: %#v\n", v)
		os.Exit(1)
	}
}
`

func TestSumType_RoundTrip(t *testing.T) {
	dir := sumTypeModuleDir(t, roundTripProgram)
	out := strings.TrimSpace(goIn(t, dir, "run", "./cmd/proof"))
	if out != `{"kind":"a","model":{"side":2}}` {
		t.Errorf("wire form = %s", out)
	}
}
