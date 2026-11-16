package plan

import (
	"slices"
	"testing"
)

func TestRoleName_Exhaustive(t *testing.T) {
	if RoleName(Generated{}) != "generated" || RoleName(Seam{}) != "seam" {
		t.Fatalf("role names: %q %q", RoleName(Generated{}), RoleName(Seam{}))
	}
}

func TestIsSeam_Exhaustive(t *testing.T) {
	if IsSeam(Generated{}) || !IsSeam(Seam{}) {
		t.Fatalf("IsSeam: generated=%v seam=%v", IsSeam(Generated{}), IsSeam(Seam{}))
	}
}

func TestPaths_FiltersByRoleAndSorts(t *testing.T) {
	outs := []Output{{"b.go", Generated{}}, {"hooks.go", Seam{}}, {"a.go", Generated{}}}
	if got := Paths(outs, Generated{}); !slices.Equal(got, []string{"a.go", "b.go"}) {
		t.Fatalf("Paths = %v", got)
	}
	if got := Paths(outs, Seam{}); !slices.Equal(got, []string{"hooks.go"}) {
		t.Fatalf("Paths(Seam) = %v", got)
	}
}

func TestOutputs_DropsContentAndSorts(t *testing.T) {
	files := []File{{Output{"z", Seam{}}, []byte("1")}, {Output{"a", Generated{}}, []byte("2")}}
	got := Outputs(files)
	if len(got) != 2 || got[0].Path != "a" || got[1].Path != "z" {
		t.Fatalf("Outputs = %v", got)
	}
}
