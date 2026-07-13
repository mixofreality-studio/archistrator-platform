package methodassets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
)

const manifestPath = ".claude/.method-assets-manifest.json"

// selfModulePath is this module's import path, used to look itself up in
// runtime/debug build info when resolving the materialized version string.
const selfModulePath = "github.com/mixofreality-studio/archistrator-platform/method-assets"

type manifest struct {
	Version string   `json:"version"`
	Files   []string `json:"files"`
}

// Materialize writes the embedded .claude tree into destRepo, prunes files
// the PREVIOUS manifest owned that no longer exist in the asset set, and
// rewrites the manifest. Files not listed in the manifest are never touched.
func Materialize(destRepo string) error {
	files, err := ClaudeFiles()
	if err != nil {
		return err
	}
	var prev manifest
	if b, err := os.ReadFile(filepath.Join(destRepo, manifestPath)); err == nil {
		_ = json.Unmarshal(b, &prev) // corrupt manifest = treat as empty
	}
	owned := map[string]bool{}
	for p, body := range files {
		owned[p] = true
		abs := filepath.Join(destRepo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return err
		}
	}
	for _, p := range prev.Files {
		if !owned[p] {
			_ = os.Remove(filepath.Join(destRepo, filepath.FromSlash(p)))
		}
	}
	next := manifest{Version: moduleVersion(), Files: make([]string, 0, len(owned))}
	for p := range owned {
		next.Files = append(next.Files, p)
	}
	sort.Strings(next.Files)
	b, _ := json.MarshalIndent(next, "", "  ")
	return os.WriteFile(filepath.Join(destRepo, manifestPath), append(b, '\n'), 0o644)
}

// moduleVersion reports this module's version via build info ("(devel)" in tests).
func moduleVersion() string { return readBuildVersion() }

// readBuildVersion resolves this module's version from runtime/debug build
// info: the main module's version if the main module IS method-assets (the
// case for the built CLI binary), else the method-assets entry in the
// dependency list (the case when method-assets is imported as a library),
// defaulting to "devel" when build info is unavailable or no version was
// recorded.
func readBuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	if info.Main.Path == selfModulePath && info.Main.Version != "" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == selfModulePath && dep.Version != "" {
			return dep.Version
		}
	}
	return "devel"
}
