package arch

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fake_test.go exercises the generated-test-double exemption (checkPackage's
// second structural skip, alongside the empty-syntax skip): Check must produce
// ZERO findings for a fully-generated <component>/fake package even though it
// imports its sibling contract package — a same-layer edge that would otherwise be
// flagged as a forbidden sideways import. The negative fixture proves the
// exemption cannot be abused: a fake-leaf package carrying ANY hand-written file
// remains fully subject to every rule (classification, sideways-import, interface
// naming).
//
// TestIsGeneratedTestDouble pins the pure predicate directly (including the
// "notfake" boundary case, which a naive strings.HasSuffix(path, "fake") would
// wrongly match); the two Check-level tests below drive the public entry point
// end-to-end against the real testdata/fakeapp module.

const fakeappPrefix = "example.com/fakeapp/internal/"

func fakeappSpec() Spec {
	return Spec{
		ModuleRoot:   "testdata/fakeapp",
		ModulePrefix: fakeappPrefix,
		Layers: []Layer{
			{Name: "ResourceAccess", DirPrefix: "resourceaccess", IfaceSuffix: regexp.MustCompile(`Access$`)},
		},
	}
}

func TestIsGeneratedTestDouble(t *testing.T) {
	cases := []struct {
		name    string
		pkgPath string
		files   []string
		want    bool
	}{
		{"fake leaf all generated", "example.com/app/internal/resourceaccess/widgetaccess/fake", []string{"fake.gen.go"}, true},
		{"fake leaf multiple generated files", "example.com/app/internal/resourceaccess/widgetaccess/fake", []string{"fake.gen.go", "extra.gen.go"}, true},
		{"fake leaf with one hand-written file", "example.com/app/internal/resourceaccess/widgetaccess/fake", []string{"fake.gen.go", "handwritten.go"}, false},
		{"fake leaf all hand-written", "example.com/app/internal/resourceaccess/widgetaccess/fake", []string{"handwritten.go"}, false},
		{"fake leaf no files", "example.com/app/internal/resourceaccess/widgetaccess/fake", nil, false},
		{"non-fake leaf all generated", "example.com/app/internal/resourceaccess/widgetaccess", []string{"contract.gen.go"}, false},
		{"leaf merely ending in fake is not the fake leaf", "example.com/app/internal/resourceaccess/notfake", []string{"x.gen.go"}, false},
		{"CompiledGoFiles carries a full path, basename is checked", "example.com/app/internal/resourceaccess/widgetaccess/fake", []string{"/abs/path/to/fake.gen.go"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pkg := &packages.Package{PkgPath: c.pkgPath, CompiledGoFiles: c.files}
			if got := isGeneratedTestDouble(pkg); got != c.want {
				t.Errorf("isGeneratedTestDouble(%q, %v) = %v, want %v", c.pkgPath, c.files, got, c.want)
			}
		})
	}
}

// TestCheck_GeneratedFakePackageExempt drives Check on ONLY the widgetaccess
// component + its generated fake/ double: fake.gen.go imports its sibling
// widgetaccess package (a same-layer edge) and exposes no Access$ interface of its
// own — both of which would fail Check WITHOUT the exemption. A real t.Errorf here
// (no subprocess) is the correct failure signal if the exemption regresses.
func TestCheck_GeneratedFakePackageExempt(t *testing.T) {
	t.Setenv("GOWORK", "off")
	spec := fakeappSpec()
	spec.Patterns = []string{"./internal/resourceaccess/widgetaccess/..."}
	Check(t, spec)
}

// handwrittenFakeSubprocessEnv marks the re-exec'd child of
// TestCheck_HandwrittenFakeLeafStillFails.
const handwrittenFakeSubprocessEnv = "ARCH_HANDWRITTEN_FAKE_SUBPROCESS"

// TestCheck_HandwrittenFakeLeafStillFails proves the exemption is a STRUCTURAL
// category, not a name-based escape hatch: othercomp/fake carries ONLY a
// hand-written file (no *.gen.go) that imports the sibling widgetaccess package —
// a forbidden sideways import within the ResourceAccess layer. Because Check
// reports via t.Errorf (which would also fail the parent if run in-process, per
// Go's subtest-failure propagation), the failing direction is observed by
// re-exec'ing this test binary — the same subprocess pattern used by
// methodcheck.TestCheck_FileLayoutViolationSurfacesThroughCheck: the child must
// FAIL, naming the sideways import.
func TestCheck_HandwrittenFakeLeafStillFails(t *testing.T) {
	if os.Getenv(handwrittenFakeSubprocessEnv) == "1" {
		// CHILD: actually drive Check; the sideways-import violation t.Errorf's and
		// fails this process — which is exactly what the parent asserts.
		t.Setenv("GOWORK", "off")
		spec := fakeappSpec()
		spec.Patterns = []string{"./internal/resourceaccess/othercomp/..."}
		Check(t, spec)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCheck_HandwrittenFakeLeafStillFails$", "-test.v")
	cmd.Env = append(os.Environ(), handwrittenFakeSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected Check to FAIL on a hand-written fake-leaf package with a sideways import; subprocess passed:\n%s", out)
	}
	if !strings.Contains(string(out), "sideways import forbidden") {
		t.Fatalf("subprocess failed, but not with the sideways-import rule:\n%s", out)
	}
}
