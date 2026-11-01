package webgen

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
func Bootstrap(app, surface string) ([]File, error) {
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
