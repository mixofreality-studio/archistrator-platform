package methodassets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializePreservesLocalExtrasAndPrunesOrphans(t *testing.T) {
	dir := t.TempDir()
	// Local extra the materializer must never touch.
	extra := filepath.Join(dir, ".claude", "skills", "grillme", "SKILL.md")
	os.MkdirAll(filepath.Dir(extra), 0o755)
	os.WriteFile(extra, []byte("local"), 0o644)

	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "system-architect.md")); err != nil {
		t.Fatal("expected agent not materialized")
	}
	if b, _ := os.ReadFile(extra); string(b) != "local" {
		t.Fatal("local extra clobbered")
	}

	// Simulate an asset removed in a future version: plant an owned orphan
	// by appending a fake entry to the manifest, then re-materialize.
	orphan := filepath.Join(dir, ".claude", "commands", "dead-command.md")
	os.WriteFile(orphan, []byte("stale"), 0o644)
	appendToManifest(t, dir, ".claude/commands/dead-command.md")
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("owned orphan not pruned")
	}
	if b, _ := os.ReadFile(extra); string(b) != "local" {
		t.Fatal("local extra clobbered on re-run")
	}
}

// appendToManifest reads the manifest JSON, appends path to files, and
// writes it back. Test helper used to simulate an owned orphan.
func appendToManifest(t *testing.T, dir, path string) {
	t.Helper()
	manifestFile := filepath.Join(dir, ".claude", ".method-assets-manifest.json")
	b, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version string   `json:"version"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m.Files = append(m.Files, path)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestFile, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
