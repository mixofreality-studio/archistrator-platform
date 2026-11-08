// Package testfixtures reads the conformance fixtures for this module's tests.
//
// The fixtures are single-sourced in the sibling framework-go-conformance module
// (testdata/<fixture>/project.json). The generator library cannot import that
// module (it requires this one, and it requires method-assets, which pins this
// one), so tests reach the fixture files by their relative path in the monorepo
// checkout. A test run from a tree without the sibling is skipped, not failed.
package testfixtures

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Fixture returns the project.json bytes of the named conformance fixture, or skips
// the calling test when the framework-go-conformance sibling is not checked out.
func Fixture(t testing.TB, name string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("testfixtures: cannot locate the monorepo checkout (runtime.Caller)")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "framework-go-conformance", "testdata", name, "project.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- a fixed sibling path plus a test-chosen fixture name
	if os.IsNotExist(err) {
		t.Skipf("conformance fixture %q not found at %s: the framework-go-conformance sibling is not in this checkout", name, path)
	}
	if err != nil {
		t.Fatalf("read conformance fixture %q: %v", name, err)
	}
	return raw
}
