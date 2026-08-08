package methodcheck

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestPrototypeAsset_NoDrift is the drift gate over the generated method asset.
// The asset is what the drafting agent reads; the Go baseline is what the rules
// judge. If the two can disagree, the template teaches agents to fail the gate.
//
// The asset lives in the sibling method-assets module (which deliberately has no
// dependencies, so it cannot import this package and generate the file itself).
// When that module is not present — this module vendored on its own — there is
// nothing to check and the test skips rather than failing for an absent
// consumer.
func TestPrototypeAsset_NoDrift(t *testing.T) {
	path := filepath.Join("..", "..", PrototypeAssetPath)
	committed, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skipf("method-assets module not present at %s; nothing to drift-check", path)
	}
	if err != nil {
		t.Fatalf("read committed asset: %v", err)
	}
	rendered, err := RenderWebAppPrototypeAsset()
	if err != nil {
		t.Fatalf("render asset: %v", err)
	}
	if !bytes.Equal(committed, rendered) {
		t.Fatalf("the committed webapp deployment prototype asset is stale; regenerate it with:\n\n\tgo -C framework-go run ./cmd/gen-deployment-prototype ..\n")
	}
}
