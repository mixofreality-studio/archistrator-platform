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
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-deployment-prototype: write %s: %v\n", dest, err)
		os.Exit(1)
	}
	fmt.Println("wrote", dest)
}
