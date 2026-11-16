// Package filesnap lets a CLI test prove which files a command wrote: snapshot the tree before
// and after, and compare what is new or changed with what the consumer's manifest names.
package filesnap

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Snap is every file under a root, by slash-separated relative path, with its content.
type Snap map[string]string

// Take reads every file under root.
func Take(t testing.TB, root string) Snap {
	t.Helper()
	snap := Snap{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 G122 -- a test's own temp tree
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		snap[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// ChangedSince is the sorted paths that are new in after or whose content differs from s.
func (s Snap) ChangedSince(after Snap) []string {
	var out []string
	for p, c := range after {
		if old, ok := s[p]; !ok || old != c {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}
