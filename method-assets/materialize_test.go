package methodassets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializePreservesLocalExtrasAndPrunesOrphans(t *testing.T) {
	dir := t.TempDir()
	// Local extra the materializer must never touch.
	extra := filepath.Join(dir, ".claude", "skills", "grillme", "SKILL.md")
	mustMkdirAll(t, filepath.Dir(extra))
	mustWriteFile(t, extra, []byte("local"))

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
	mustWriteFile(t, orphan, []byte("stale"))
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

// mustMkdirAll creates dir (and any missing parents) or fails the test.
// Test helper: sidesteps errcheck noise from the repeated os.MkdirAll calls
// test setup needs to plant fixtures.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
}

// mustWriteFile writes content to path or fails the test. Test helper:
// sidesteps errcheck noise from the repeated os.WriteFile calls test setup
// needs to plant fixtures.
func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// appendToManifest reads the manifest JSON, appends path to files, and
// writes it back. Test helper used to simulate an owned orphan.
func appendToManifest(t *testing.T, dir, path string) {
	t.Helper()
	m := readManifest(t, dir)
	m.Files = append(m.Files, path)
	writeManifest(t, dir, m)
}

// readManifest reads and decodes the manifest written into dir/.claude.
func readManifest(t *testing.T, dir string) manifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// writeManifest encodes m and writes it into dir/.claude, overwriting
// whatever manifest Materialize last wrote. Test helper used to simulate a
// hand-edited manifest.
func writeManifest(t *testing.T, dir string, m manifest) {
	t.Helper()
	manifestFile := filepath.Join(dir, manifestPath)
	mustMkdirAll(t, filepath.Dir(manifestFile))
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestFile, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The manifest describes files Materialize wrote FOR the caller; it is
// itself written directly by Materialize, not via the embedded asset set
// (ClaudeFiles never yields the manifest's own path). Guard that invariant
// explicitly: a manifest that lists itself would make the materializer
// prune (or attempt to prune) its own bookkeeping file.
func TestMaterializeManifestDoesNotListItself(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	for _, p := range m.Files {
		if p == manifestPath {
			t.Fatalf("manifest lists itself (%s) as an owned file", manifestPath)
		}
	}
}

// Every asset ClaudeFiles() reports must actually land on disk under dest —
// not spot-checked, but the full set, so a future asset that fails to
// materialize (e.g. a path bug) doesn't slip past a partial assertion.
func TestMaterializeWritesEveryClaudeFile(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("ClaudeFiles() returned no files; test would pass vacuously")
	}
	for p, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Errorf("%s: not written to dest: %v", p, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s: content mismatch after materialize", p)
		}
	}
}

// A hand-edited manifest that points outside the owned .claude/ tree must
// never cause Materialize to delete the target file. Chosen behavior: the
// skip is SURFACED — Materialize still writes the manifest and all owned
// assets, but returns a non-nil error naming the rejected entry, rather
// than silently ignoring a manifest tampered with (or corrupted) to point
// outside the tree.
func TestMaterializePruneContainmentRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}

	// A file that lives entirely outside dest, in an unrelated temp dir.
	// Compute the manifest entry as a relative ".."-path from dest to it so
	// the test doesn't depend on t.TempDir()'s nesting depth.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, outside)
	if err != nil {
		t.Fatal(err)
	}
	traversalEntry := filepath.ToSlash(rel)
	appendToManifest(t, dir, traversalEntry)

	err = Materialize(dir)
	if err == nil {
		t.Fatal("expected Materialize to surface an error for the traversal manifest entry, got nil")
	}
	if !strings.Contains(err.Error(), traversalEntry) {
		t.Fatalf("expected error to name the rejected entry %q, got: %v", traversalEntry, err)
	}

	if b, rerr := os.ReadFile(outside); rerr != nil || string(b) != "must survive" {
		t.Fatalf("traversal entry was deleted or modified outside dest repo: err=%v content=%q", rerr, b)
	}

	// The owned .claude tree must still be complete despite the surfaced
	// error — a rejected prune entry must not abort materialization.
	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "system-architect.md")); err != nil {
		t.Fatalf("materialization aborted by unrelated prune-containment error: %v", err)
	}
}

// A corrupt manifest must fail loudly, naming the manifest path, rather
// than being silently treated as empty (which would make every
// previously-owned file look unowned and orphan it permanently, since this
// materializer never re-adopts what it doesn't already know about).
func TestMaterializeCorruptManifestReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestFile := filepath.Join(dir, manifestPath)
	mustMkdirAll(t, filepath.Dir(manifestFile))
	mustWriteFile(t, manifestFile, []byte("{not valid json"))

	err := Materialize(dir)
	if err == nil {
		t.Fatal("expected Materialize to error on a corrupt manifest, got nil")
	}
	if !strings.Contains(err.Error(), manifestFile) {
		t.Fatalf("expected error to name the manifest path %s, got: %v", manifestFile, err)
	}
}
