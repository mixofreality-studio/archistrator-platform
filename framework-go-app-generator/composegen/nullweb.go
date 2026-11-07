package composegen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// NullFile is the generated null-operation middleware of a container that serves web-exposed
// Managers, beside main.gen.go. The null-subject run (testgen -discriminate) boots the server with
// AIARCH_NULL_OPS naming operations ("<Iface>.<Op>"); the generated REST handlers and MCP tools both
// call the Manager interfaces, so wrapping those answers a named operation with its zero result (a
// 200 with the zero body, no Manager call) over every transport at once.
const NullFile = "null.gen.go"

// nullOpsEnv is the variable carrying the operations to null; testgen exports it for the Go test
// process and for the booted server alike.
const nullOpsEnv = "AIARCH_NULL_OPS"

// localProfile is the only profile the middleware installs under.
const localProfile = "local"

// emitNullWeb renders NullFile for r's web-exposed Managers: their Null wrappers (the same ones the
// scenario subject uses) and applyNullOps, which RunGenerated calls once, right after the logging
// wrap, so the null wrapper is outermost.
func emitNullWeb(m *projectmodel.Model, r *resolved) ([]byte, error) {
	g := &scenarioGen{m: m, r: r, imp: newImportSet(), byKey: map[string]scenarioComp{}}
	for _, mc := range r.webMgrs {
		c := m.Contracts[mc.key]
		if c == nil || c.Doc == nil {
			return nil, fmt.Errorf("composegen: null middleware: manager %q has no contract document to wrap", mc.key)
		}
		g.imp.add(mc.importPath, importAliasFor(mc.importPath, mc.alias))
		comp := scenarioComp{key: mc.key, iface: mc.iface, alias: mc.alias, importPath: mc.importPath, contract: c}
		if err := g.emitNull(comp); err != nil {
			return nil, err
		}
	}
	for _, p := range []string{"fmt", "os", "strings"} {
		g.imp.add(p, "")
	}
	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + r.pkgName + "\n\n")
	b.WriteString("// The null-operation middleware of the null-subject run (testgen -discriminate): with\n")
	b.WriteString("// " + nullOpsEnv + " naming operations, the generated transports answer them with their zero\n")
	b.WriteString("// result and never reach the Manager. A test instrument: the local profile only.\n\n")
	b.WriteString(g.imp.render())
	b.WriteString(nullOpsSource)
	b.WriteString(g.body.String())
	b.WriteString(applyNullOpsSource(r))
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, wrapFormatErr(err, b.String())
	}
	return src, nil
}

// applyNullOpsSource renders applyNullOps(profile, managers): the bundle unchanged when the variable is
// unset; an error under any profile but local; otherwise each Manager in its Null wrapper.
func applyNullOpsSource(r *resolved) string {
	var b strings.Builder
	b.WriteString("// applyNullOps wraps each web-exposed Manager in its Null wrapper for the operations\n")
	b.WriteString("// " + nullOpsEnv + " names. Unset, it returns managers unchanged under every profile; set\n")
	b.WriteString("// under any profile but " + localProfile + ", it refuses, so a deployed binary cannot be told to stub out\n")
	b.WriteString("// its own operations.\n")
	b.WriteString("func applyNullOps(profile string, managers WebManagers) (WebManagers, error) {\n")
	fmt.Fprintf(&b, "\tspec := os.Getenv(%q)\n", nullOpsEnv)
	b.WriteString("\tif spec == \"\" {\n\t\treturn managers, nil\n\t}\n")
	fmt.Fprintf(&b, "\tif profile != %q {\n", localProfile)
	fmt.Fprintf(&b, "\t\treturn managers, fmt.Errorf(\"%s is set under profile %%q: null operations are a %s-profile test instrument, so the process refuses to start\", profile)\n\t}\n", nullOpsEnv, localProfile)
	b.WriteString("\tops := strings.Split(spec, \",\")\n")
	for _, mc := range r.webMgrs {
		fmt.Fprintf(&b, "\tmanagers.%s = Null%s(managers.%s, ops...)\n", mc.iface, mc.iface, mc.iface)
	}
	b.WriteString("\treturn managers, nil\n}\n")
	return b.String()
}
