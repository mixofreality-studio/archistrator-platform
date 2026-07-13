// Package methodassets owns every file archistrator seats into an app repo:
// the .claude agents/commands/skills tree, both GitHub workflow templates,
// and the scaffold file templates. Consumers: the archistrator server
// (managed scaffold), the cmd/method-assets materializer, and archistrator's
// own repo (dogfooding via the materializer + a CI drift gate).
package methodassets

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed all:assets
var assetsFS embed.FS

// ClaudeFiles returns the full .claude tree as repo-relative path -> bytes
// (".claude/agents/system-architect.md", ...). The map is rebuilt per call;
// callers may mutate it.
func ClaudeFiles() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(assetsFS, "assets/claude", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) == ".gitkeep" {
			return err
		}
		body, rerr := assetsFS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel := ".claude/" + p[len("assets/claude/"):]
		out[rel] = body
		return nil
	})
	return out, err
}
