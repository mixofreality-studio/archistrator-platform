package arch

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PlatformModulePrefix is the import prefix of the platform's own module family.
const PlatformModulePrefix = "github.com/mixofreality-studio/archistrator-platform/"

// sanctionedDrivers is the framework's fixed infrastructure menu for every app built
// with archistrator (the CustomerAppInfrastructure volatility made executable): the
// drivers the framework-go infrastructure modules and the generators' output carry.
var sanctionedDrivers = []string{
	"github.com/google/uuid",                 // sanctioned identity type
	"github.com/jackc/pgx",                   // Postgres
	"github.com/go-git/",                     // Git/Gitea
	"go.temporal.io/",                        // the durable-execution substrate
	"github.com/modelcontextprotocol/go-sdk", // the generated MCP client surface
	"github.com/google/jsonschema-go",        // the MCP SDK's schema type, carried by the generated tools
}

// DefaultAllowedImportPrefixes is the dependency menu every app starts from: the
// platform family, the app's own module (when known) and the sanctioned drivers.
// Production code importing anything else fails Check's allowlist rule. An unknown
// module ("") contributes no own-module entry.
func DefaultAllowedImportPrefixes(module string) []string {
	out := []string{PlatformModulePrefix}
	if module != "" {
		out = append(out, module+"/")
	}
	return append(out, sanctionedDrivers...)
}

// ModulePathOf reads the module path out of <moduleRoot>/go.mod.
func ModulePathOf(moduleRoot string) (string, error) {
	path := filepath.Join(moduleRoot, "go.mod")
	raw, err := os.ReadFile(path) //nolint:gosec // moduleRoot is the consuming test's own constant
	if err != nil {
		return "", fmt.Errorf("derive the module path: %w", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		fields := strings.Fields(stripModComment(sc.Text()))
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", fmt.Errorf("derive the module path: %s has no module line", path)
}

// CheckModuleFile holds the committed go.mod to what the job's own `go mod tidy` can
// produce and every clone can resolve: no toolchain line (the job tidies under
// GOTOOLCHAIN=local) and no replace pointing at a local path (which builds here and
// nowhere else).
func CheckModuleFile(t *testing.T, moduleRoot string) {
	t.Helper()
	path := filepath.Join(moduleRoot, "go.mod")
	raw, err := os.ReadFile(path) //nolint:gosec // moduleRoot is the consuming test's own constant
	if err != nil {
		t.Fatalf("arch: %v", err)
	}
	for _, v := range moduleFileViolations(raw) {
		t.Errorf("%s: %s", path, v)
	}
}

// moduleFileViolations is CheckModuleFile's pure core.
func moduleFileViolations(gomod []byte) []string {
	var out []string
	inReplace := false
	sc := bufio.NewScanner(bytes.NewReader(gomod))
	for sc.Scan() {
		fields := strings.Fields(stripModComment(sc.Text()))
		switch {
		case len(fields) == 0:
		case inReplace && fields[0] == ")":
			inReplace = false
		case inReplace:
			out = append(out, localReplaceViolations(fields)...)
		case fields[0] == "toolchain":
			out = append(out, "a toolchain line is not allowed: the module builds with the job's toolchain ("+strings.Join(fields, " ")+")")
		case fields[0] == "replace" && len(fields) > 1 && fields[1] == "(":
			inReplace = true
		case fields[0] == "replace":
			out = append(out, localReplaceViolations(fields[1:])...)
		}
	}
	return out
}

// localReplaceViolations reports a replace directive (`old [v] => new [v]`) whose
// target is a filesystem path.
func localReplaceViolations(fields []string) []string {
	for i, f := range fields {
		if f != "=>" || i+1 >= len(fields) {
			continue
		}
		target := strings.Trim(fields[i+1], `"`)
		if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
			return []string{"a replace to a local path is not allowed: " + strings.Join(fields, " ")}
		}
	}
	return nil
}

func stripModComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}
