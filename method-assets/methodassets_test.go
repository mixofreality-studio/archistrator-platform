package methodassets

import (
	"strings"
	"testing"
)

func TestClaudeFilesKeysAreRepoRelative(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("ClaudeFiles returned no files")
	}
	for path, body := range files {
		if !strings.HasPrefix(path, ".claude/") {
			t.Errorf("key %q must start with .claude/", path)
		}
		if len(body) == 0 {
			t.Errorf("file %q is empty", path)
		}
	}
}
