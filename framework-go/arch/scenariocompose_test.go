package arch

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestIsGeneratedScenarioCompose pins the structural exemption that keeps the
// generated internal/scenariocompose package out of the layer model: exactly
// that directory under the module prefix, and only while every compiled file is
// generated.
func TestIsGeneratedScenarioCompose(t *testing.T) {
	spec := Spec{ModulePrefix: "example.com/app/internal/"}
	for _, c := range []struct {
		name  string
		path  string
		files []string
		want  bool
	}{
		{"generated", "example.com/app/internal/scenariocompose", []string{"/x/compose.gen.go"}, true},
		{"one hand file", "example.com/app/internal/scenariocompose", []string{"/x/compose.gen.go", "/x/helper.go"}, false},
		{"no files", "example.com/app/internal/scenariocompose", nil, false},
		{"another package", "example.com/app/internal/manager/scenariocompose", []string{"/x/compose.gen.go"}, false},
		{"nested", "example.com/app/internal/scenariocompose/sub", []string{"/x/a.gen.go"}, false},
	} {
		pkg := &packages.Package{PkgPath: c.path, CompiledGoFiles: c.files}
		if got := isGeneratedScenarioCompose(pkg, spec); got != c.want {
			t.Errorf("%s: isGeneratedScenarioCompose = %v, want %v", c.name, got, c.want)
		}
	}
	if isGeneratedScenarioCompose(&packages.Package{PkgPath: "scenariocompose", CompiledGoFiles: []string{"a.gen.go"}}, Spec{}) {
		t.Error("a spec with no module prefix must never exempt")
	}
}
