package methodassets

import (
	"sort"
	"strings"
	"testing"
)

// TestLayoutNeutral asserts the embedded .claude construction assets carry no
// archistrator-repo-layout assumptions and no dead relative book-file links, so
// the same assets work in archistrator's multi-dir repo AND a generated app's
// module-root repo.
//
//   - `server/internal/` — hardcodes archistrator's server subdirectory layout.
//     Layout is derived from committed state (the contract's goPackage) or
//     expressed module-root-relative instead.
//   - relative book-file links (`rightingsoftware/...` paths, `.xhtml` files) —
//     dead once the assets ship in an app repo that has no research corpus.
//     Plain chapter citations ("Löwy ch. 9 §5", "App C §6") stay legal; only the
//     file paths are banned.
func TestLayoutNeutral(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}

	// Substrings that betray a hardcoded layout or a dead book-file link.
	banned := []string{
		"server/internal/", // archistrator server subdirectory
		"rightingsoftware", // relative path into the book corpus
		".xhtml",           // book source file extension
	}

	type offense struct {
		file  string
		token string
		count int
	}
	var offenses []offense
	for path, body := range files {
		text := string(body)
		for _, tok := range banned {
			if n := strings.Count(text, tok); n > 0 {
				offenses = append(offenses, offense{path, tok, n})
			}
		}
	}

	if len(offenses) > 0 {
		sort.Slice(offenses, func(i, j int) bool {
			if offenses[i].file != offenses[j].file {
				return offenses[i].file < offenses[j].file
			}
			return offenses[i].token < offenses[j].token
		})
		total := 0
		for _, o := range offenses {
			total += o.count
			t.Errorf("%s: %d× %q", o.file, o.count, o.token)
		}
		t.Fatalf("layout-neutral: %d banned occurrences across %d file/token pairs", total, len(offenses))
	}
}
