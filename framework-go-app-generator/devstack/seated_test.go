package devstack

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/testfixtures"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// The SPA probe runs iff the web project is seated: a browser surface whose web client has not
// been constructed is not yet built, so an integration before it must not be held to serve it.
func TestSmokeSpec_SPAIffSeated(t *testing.T) {
	m, err := projectmodel.Load(testfixtures.Fixture(t, "todomvc"))
	if err != nil {
		t.Fatal(err)
	}
	if !m.System.HasBrowserSurface() {
		t.Fatal("the todomvc fixture has a browser surface")
	}
	dir := t.TempDir()
	if WebSeated(dir) {
		t.Error("an empty module has no seated web project")
	}
	if spec := SmokeSpecFor(m, dir, nil); spec.SPA {
		t.Error("a browser surface with no webApp/package.json must not be probed for its SPA")
	}
	if err := os.MkdirAll(filepath.Join(dir, "webApp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if WebSeated(dir) {
		t.Error("a webApp directory without package.json is not a seated project")
	}
	if err := os.WriteFile(filepath.Join(dir, "webApp", "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := SmokeSpecFor(m, dir, nil)
	if !WebSeated(dir) || !spec.SPA {
		t.Errorf("a seated web project must be probed: seated=%v spec=%+v", WebSeated(dir), spec)
	}
	if !slices.Equal(spec.Tools, ToolNames(m, nil)) {
		t.Errorf("Tools = %v, want ToolNames", spec.Tools)
	}
	// A model with no browser surface is never probed for one, seated or not.
	if spec := SmokeSpecFor(&projectmodel.Model{System: &projectmodel.System{}}, dir, nil); spec.SPA {
		t.Error("no browser surface: no SPA probe")
	}
}
