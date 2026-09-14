// Command gen-deployment-prototype writes the web-app deployment prototype
// method asset from methodcheck.WebAppBaseline().
//
// The asset the drafting agent starts from and the DEP-EDGE-GATEWAY rule that
// judges the result are two consumers of ONE definition. Generating the asset is
// what keeps them from drifting: a hand-authored template would go stale the
// first time the front door changed, and would then be teaching agents to fail
// the gate.
//
// Usage (from the archistrator-platform root):
//
//	go -C framework-go run ./cmd/gen-deployment-prototype ..
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

func main() {
	root := ".."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	body, err := methodcheck.RenderWebAppPrototypeAsset()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dest := filepath.Join(root, methodcheck.PrototypeAssetPath)
	if err := writeAsset(root, body); err != nil {
		fmt.Fprintf(os.Stderr, "gen-deployment-prototype: write %s: %v\n", dest, err)
		os.Exit(1)
	}
	fmt.Println("wrote", dest)
}

// writeAsset writes the asset at its fixed path under the platform root.
//
// The root comes from the command line, so it is opened as an os.Root and the
// write goes through that root. The fixed, repo-relative PrototypeAssetPath
// therefore cannot resolve outside the directory the operator named, even
// through a symlink.
//
// The mode is 0o600, like every other generator in this repo (httpgen, mcpgen,
// webgen, gen-stepmanifest). The asset is committed source, and git records
// only the executable bit, never read/write permissions, so this mode never
// reaches the tracked file or any other checkout. The mode is applied only
// when the write CREATES the file; regenerating an existing checkout keeps
// whatever mode the file already has. So it matters only for a fresh file on
// this developer's disk, and owner-only is the right default there.
func writeAsset(root string, body []byte) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return r.WriteFile(methodcheck.PrototypeAssetPath, body, 0o600)
}
