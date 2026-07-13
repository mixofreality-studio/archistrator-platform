package methodassets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
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
//
// The materializer only ever deletes files it owns, inside destRepo: prune
// entries are validated (see isSafeManifestPath) before any os.Remove, and a
// corrupt manifest is a hard error rather than a silent empty-manifest that
// would orphan every previously-owned file.
func Materialize(destRepo string) error {
	files, err := ClaudeFiles()
	if err != nil {
		return err
	}

	manifestFile := filepath.Join(destRepo, manifestPath)
	var prev manifest
	if b, rerr := os.ReadFile(manifestFile); rerr == nil {
		if uerr := json.Unmarshal(b, &prev); uerr != nil {
			// Silently treating a corrupt manifest as empty would make every
			// file it previously owned look "unowned" and orphan them
			// forever (this materializer never re-adopts what it doesn't
			// know about). Fail loudly and name the fix instead.
			return fmt.Errorf("materialize: manifest %s is corrupt: %w (delete it to re-adopt all owned files)", manifestFile, uerr)
		}
	} else if !os.IsNotExist(rerr) {
		return rerr
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

	var errs []error
	for _, p := range prev.Files {
		if owned[p] {
			continue
		}
		if !isSafeManifestPath(p) {
			// A hand-edited or corrupt manifest must not be able to direct
			// deletes outside the .claude tree this materializer owns.
			// Surface it and drop the bogus entry rather than retry it
			// forever or risk ever calling os.Remove on it.
			errs = append(errs, fmt.Errorf("materialize: refusing to prune manifest entry outside owned tree: %q", p))
			continue
		}
		if rerr := os.Remove(filepath.Join(destRepo, filepath.FromSlash(p))); rerr != nil && !os.IsNotExist(rerr) {
			errs = append(errs, fmt.Errorf("materialize: failed to prune %s: %w", p, rerr))
			// Keep it in the manifest so the next run retries the prune
			// instead of orphaning the file (self-healing).
			owned[p] = true
		}
	}

	next := manifest{Version: moduleVersion(), Files: make([]string, 0, len(owned))}
	for p := range owned {
		next.Files = append(next.Files, p)
	}
	sort.Strings(next.Files)
	b, merr := json.MarshalIndent(next, "", "  ")
	if merr != nil {
		return merr
	}
	if werr := os.WriteFile(manifestFile, append(b, '\n'), 0o644); werr != nil {
		return werr
	}
	return errors.Join(errs...)
}

// isSafeManifestPath reports whether p is safe to treat as a materializer-
// owned path for pruning: a clean, relative path rooted under ".claude/"
// with no ".." segments. The manifest is read back from disk on every run,
// so a hand-edited or corrupt manifest must never be able to direct
// os.Remove outside the tree this materializer owns.
func isSafeManifestPath(p string) bool {
	if p != path.Clean(p) {
		return false
	}
	if path.IsAbs(p) {
		return false
	}
	if !strings.HasPrefix(p, ".claude/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
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
