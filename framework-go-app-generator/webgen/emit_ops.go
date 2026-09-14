package webgen

import (
	"fmt"
	"strings"
)

const opBindingsPlaceholder = "@@OP_BINDINGS@@"

// EmitOps renders src/api/ops.gen.ts: the OpsClient interface, OP_BINDINGS and
// the REST and MCP transports. The fixture transport stays out of this file on
// purpose: it lives in src/api/fixtureOps.ts over framework-web's preview
// runtime, so a production bundle, which never imports that module, can be
// checked to carry none of it.
//
// The text is exactly what the reference generator (archistrator's
// scripts/gen-ops.mjs followed by prettier) wrote at bcb20abf, apart from the
// six comment lines that name the generator; the archistrator golden test pins
// that.
func EmitOps(bindings []Binding) ([]byte, error) {
	tmpl, err := templates.ReadFile("templates/ops.gen.ts.tmpl")
	if err != nil {
		return nil, err
	}
	if strings.Count(string(tmpl), opBindingsPlaceholder) != 1 {
		return nil, fmt.Errorf("webgen: ops template must hold exactly one %s", opBindingsPlaceholder)
	}
	return []byte(strings.Replace(string(tmpl), opBindingsPlaceholder, opBindingsLiteral(bindings), 1)), nil
}

// opBindingsLiteral writes the table the way prettier prints the
// JSON.stringify(bindings, null, 2) the reference emitted: unquoted keys, single
// quotes, one property per line, trailing commas (trailingComma: es5).
func opBindingsLiteral(bindings []Binding) string {
	var b strings.Builder
	b.WriteString("{\n")
	for _, x := range bindings {
		fmt.Fprintf(&b, "  %s: {\n", x.OpID)
		fmt.Fprintf(&b, "    method: %s,\n", singleQuoted(x.Method))
		fmt.Fprintf(&b, "    path: %s,\n", singleQuoted(x.Path))
		if x.Tool == "" {
			b.WriteString("    tool: null,\n")
		} else {
			fmt.Fprintf(&b, "    tool: %s,\n", singleQuoted(x.Tool))
		}
		if x.Composition {
			b.WriteString("    composition: true,\n")
		}
		b.WriteString("  },\n")
	}
	b.WriteString("}")
	return b.String()
}

// singleQuoted is a TS string literal in prettier's single-quote style.
func singleQuoted(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)
	return "'" + r.Replace(s) + "'"
}
