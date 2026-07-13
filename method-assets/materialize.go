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
	prev, err := loadPrevManifest(manifestFile)
	if err != nil {
		return err
	}

	owned := map[string]bool{}
	for p := range files {
		owned[p] = true
	}
	if err := writeAssets(destRepo, files); err != nil {
		return err
	}

	// Prune failures are self-healing: a path that failed to remove is kept
	// in owned so it round-trips back into the manifest and this function
	// retries it on the next run, instead of orphaning it.
	retained, errs := pruneOrphans(destRepo, prev, owned)
	for _, p := range retained {
		owned[p] = true
	}

	// buildManifest only reads keys, so a retained path with no known content
	// (files[p] is nil for it) still round-trips into the manifest correctly.
	ownedFiles := make(map[string][]byte, len(owned))
	for p := range owned {
		ownedFiles[p] = files[p]
	}
	b, merr := json.MarshalIndent(buildManifest(ownedFiles), "", "  ")
	if merr != nil {
		return merr
	}
	if werr := os.WriteFile(manifestFile, append(b, '\n'), 0o600); werr != nil {
		return werr
	}
	return errors.Join(errs...)
}

// loadPrevManifest reads and decodes the manifest at manifestFile. A missing
// manifest is the first-run case and yields an empty manifest; a corrupt
// manifest is a hard error naming the path rather than being silently
// treated as empty, which would make every file it previously owned look
// "unowned" and orphan them forever (this materializer never re-adopts what
// it doesn't already know about).
func loadPrevManifest(manifestFile string) (manifest, error) {
	var prev manifest
	// #nosec G304 -- manifestFile is destRepo-rooted (filepath.Join(destRepo,
	// manifestPath) in Materialize), not attacker- or user-supplied input.
	b, rerr := os.ReadFile(manifestFile)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return prev, nil
		}
		return prev, rerr
	}
	if uerr := json.Unmarshal(b, &prev); uerr != nil {
		return prev, fmt.Errorf("materialize: manifest %s is corrupt: %w (delete it to re-adopt all owned files)", manifestFile, uerr)
	}
	return prev, nil
}

// writeAssets writes every embedded .claude asset into destRepo, creating
// parent directories as needed.
func writeAssets(destRepo string, files map[string][]byte) error {
	for p, body := range files {
		abs := filepath.Join(destRepo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(abs, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// pruneOrphans removes files prev owned that owned (the current asset set)
// no longer claims. A prune entry outside the owned .claude/ tree is refused
// and surfaced as an error rather than removed — containment against a
// hand-edited or corrupt manifest directing deletes outside the tree this
// materializer owns — and the bogus entry is dropped rather than retried.
// A prune entry whose removal fails is returned in retained so the caller
// can keep it in the manifest and retry on the next run instead of
// orphaning the file (self-healing).
func pruneOrphans(destRepo string, prev manifest, owned map[string]bool) (retained []string, errs []error) {
	for _, p := range prev.Files {
		if owned[p] {
			continue
		}
		if !isSafeManifestPath(p) {
			errs = append(errs, fmt.Errorf("materialize: refusing to prune manifest entry outside owned tree: %q", p))
			continue
		}
		if rerr := os.Remove(filepath.Join(destRepo, filepath.FromSlash(p))); rerr != nil && !os.IsNotExist(rerr) {
			errs = append(errs, fmt.Errorf("materialize: failed to prune %s: %w", p, rerr))
			retained = append(retained, p)
		}
	}
	return retained, errs
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

// buildManifest builds the manifest for a file set: the module version plus
// the sorted list of files' keys. Content is never inspected — callers may
// pass nil bodies for keys whose content isn't at hand (e.g. Materialize's
// retained-but-unwritten orphans). Shared by Materialize and ScaffoldFiles so
// the manifest shape is defined in exactly one place.
func buildManifest(files map[string][]byte) manifest {
	m := manifest{Version: Version(), Files: make([]string, 0, len(files))}
	for p := range files {
		m.Files = append(m.Files, p)
	}
	sort.Strings(m.Files)
	return m
}

// Version reports this module's version (build-info derived; "devel" outside
// a versioned build). Exported so callers — the archistrator server's
// SyncManagedScaffold in particular — can fingerprint a seated .claude/**
// tree against the manifest's version without re-deriving it from build info
// themselves.
func Version() string { return readBuildVersion() }

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
