package webgen

import (
	"fmt"
	"strings"
)

// Bootstrap renders the web project a fresh app's first frontend job starts from, and the
// browser surface it is building. It is written through WriteScaffold, so what exists stays:
// the second surface adds only its own files, and every seat is the app's from then on.
//
// All browser surfaces share one project, with one toolchain and one entry each:
//
//	<surface>.html, src/<surface>/main.tsx, src/<surface>/routes/App.tsx   the surface
//	src/shared/                                                            code they share
//	src/api/, src/contracts/                                               the transport seam
//
// The preview shell is left out for now (webgen.json preview: false): it imports
// framework-web's preview entry points, which the published package does not carry yet.
//
// EVERY SHARED SEAT IS SURFACE-NEUTRAL (FIX-036): two surfaces bootstrapped in parallel on
// separate branches must write byte-identical shared files, or their merges conflict
// (webgen.json did, add/add, while it carried "surface"). Only the surface's own three files
// are rendered from the surface name.
func Bootstrap(app, surface string) ([]File, error) {
	if !validSurface(surface) {
		return nil, fmt.Errorf("webgen: bootstrap: surface %q must be kebab-case, at most %d characters", surface, maxSurface)
	}
	c := Config{App: app, Surface: surface, OAS: "-", MCPTools: "-", FixturesEnv: DefaultFixturesEnv(app)}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var out []File
	for _, f := range bootstrapFiles(surface) {
		content, err := render(f.tmpl, c)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: f.out, Content: content})
	}
	return out, nil
}

// AppSlug is a module path's app name in the shape Config.App requires: its last segment
// (the one before a trailing "server"), lower-cased, with anything else as '-'.
func AppSlug(module string) string {
	segs := strings.Split(module, "/")
	leaf := segs[len(segs)-1]
	if leaf == "server" && len(segs) > 1 {
		leaf = segs[len(segs)-2]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(leaf) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String()[:min(b.Len(), 40)], "-")
}

func bootstrapFiles(surface string) []struct{ tmpl, out string } {
	const b = "bootstrap/"
	return []struct{ tmpl, out string }{
		{b + "webgen.json.tmpl", ConfigFile},
		{b + "package.json.tmpl", "package.json"},
		{b + "tsconfig.json.tmpl", "tsconfig.json"},
		{b + "tsconfig.app.json.tmpl", "tsconfig.app.json"},
		{b + "vite.config.ts.tmpl", "vite.config.ts"},
		{b + "eslint.config.js.tmpl", "eslint.config.js"},
		{b + "eslint.platform.config.js.tmpl", "eslint.platform.config.js"},
		{b + "gitignore.tmpl", ".gitignore"},
		{b + "tool-versions.tmpl", ".tool-versions"},
		{b + "vite-env.d.ts.tmpl", "src/vite-env.d.ts"},
		{"errors.ts.tmpl", "src/contracts/errors.ts"},
		{"opsContext.tsx.tmpl", "src/api/opsContext.tsx"},
		{"opTypes.ts.tmpl", "src/api/opTypes.ts"},
		{b + "surface.html.tmpl", surface + ".html"},
		{b + "main.tsx.tmpl", "src/" + surface + "/main.tsx"},
		{b + "App.tsx.tmpl", "src/" + surface + "/routes/App.tsx"},
		{b + "uitests.package.json.tmpl", "../uitests/package.json"},
		{b + "uitests.gitignore.tmpl", "../uitests/.gitignore"},
		{b + "tool-versions.tmpl", "../uitests/.tool-versions"},
	}
}
