package conformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A launcher that dies before it is ready must hand its own output to the caller:
// the CI log is otherwise just "exit status 1".
func TestBoot_ExitBeforeReadyCarriesTheLauncherLog(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'fake: temporal CLI missing' >&2\necho 'fake stdout line'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o700); err != nil { // #nosec G306 -- test stub must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := Boot(context.Background(), dir)
	if err == nil {
		t.Fatal("Boot succeeded against a launcher that exits 1")
	}
	for _, want := range []string{"exited before it was ready", "fake: temporal CLI missing", "fake stdout line"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}
