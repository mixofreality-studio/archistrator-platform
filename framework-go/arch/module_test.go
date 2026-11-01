package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMethodSpecDerivesPrefixFromGoMod(t *testing.T) {
	spec := MethodSpec("testdata/layoutapp")
	if spec.ModulePrefix != "example.com/layoutapp/internal/" {
		t.Fatalf("ModulePrefix = %q", spec.ModulePrefix)
	}
	if !slices.Contains(spec.AllowedImportPrefixes, "example.com/layoutapp/") ||
		!slices.Contains(spec.AllowedImportPrefixes, PlatformModulePrefix) ||
		!slices.Contains(spec.AllowedImportPrefixes, "github.com/jackc/pgx") {
		t.Errorf("the framework menu is not the default: %v", spec.AllowedImportPrefixes)
	}
}

// The menu is the fixed infrastructure every app builds on. Widening it is a decision, so it
// is spelled here: a stray prefix (or a whole host) must not slip in with an unrelated change.
func TestDefaultMenuIsExactlyTheSanctionedSet(t *testing.T) {
	want := []string{
		PlatformModulePrefix,
		"example.com/app/",
		"github.com/google/uuid",
		"github.com/jackc/pgx",
		"github.com/go-git/",
		"go.temporal.io/",
		"github.com/modelcontextprotocol/go-sdk",
		"github.com/google/jsonschema-go",
	}
	if got := DefaultAllowedImportPrefixes("example.com/app"); !slices.Equal(got, want) {
		t.Fatalf("the dependency menu changed:\n got %v\nwant %v", got, want)
	}
}

func TestMethodSpecRecordsAnUnreadableModule(t *testing.T) {
	if spec := MethodSpec(t.TempDir()); spec.loadErr == nil || spec.ModulePrefix != "" {
		t.Fatalf("a directory with no go.mod must not yield a usable spec: %+v", spec)
	}
}

func TestModulePathOf(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("// head\nmodule \"example.com/q\" // c\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ModulePathOf(dir)
	if err != nil || got != "example.com/q" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestModuleFileViolations(t *testing.T) {
	for _, tc := range []struct {
		name, mod string
		want      []string
	}{
		{"clean", "module a\n\ngo 1.26.0\n\nrequire b v1.0.0\n", nil},
		{"toolchain", "module a\ntoolchain go1.26.3\n", []string{"toolchain"}},
		{"local replace", "module a\nreplace b => ../b\n", []string{"../b"}},
		{"block local replace", "module a\nreplace (\n\tb v1.0.0 => ./b\n\tc => d v1.0.0\n\te => /tmp/e\n)\n", []string{"./b", "/tmp/e"}},
		{"remote replace is fine", "module a\nreplace b => github.com/x/b v1.2.3\n", nil},
		{"a commented line is not a directive", "module a\n// toolchain go1.26.3\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := moduleFileViolations([]byte(tc.mod))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %d violations naming %v", got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("violation %q does not name %q", got[i], w)
				}
			}
		})
	}
}
