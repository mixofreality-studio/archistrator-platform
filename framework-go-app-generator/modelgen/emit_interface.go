package modelgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// decodeAndEmitInterface re-decodes the document's `interface` key into the typed
// descriptor and emits the Go interface. Reports whether an interface was present.
func decodeAndEmitInterface(buf *bytes.Buffer, doc *jsonschema.Schema) (Interface, bool, error) {
	var iface Interface
	ix, ok := doc.Extra["interface"]
	if !ok {
		return iface, false, nil
	}
	ib, err := json.Marshal(ix)
	if err != nil {
		return iface, false, fmt.Errorf("re-marshal interface: %w", err)
	}
	if err := json.Unmarshal(ib, &iface); err != nil {
		return iface, false, fmt.Errorf("decode interface: %w", err)
	}
	emitInterface(buf, iface)
	return iface, true, nil
}

// emitInterface writes the component's service-contract interface and its ops.
// Every method takes the layer's call Context (`rc`) as its first parameter.
func emitInterface(buf *bytes.Buffer, iface Interface) {
	if iface.Name == "" {
		return
	}
	lc, hasLayer := layerContext[iface.Layer]
	fmt.Fprintf(buf, "// %s is the generated service-contract interface for this component.\n", iface.Name)
	fmt.Fprintf(buf, "type %s interface {\n", iface.Name)
	for _, op := range iface.Operations {
		params := make([]string, 0, len(op.Params)+1)
		if hasLayer {
			params = append(params, "rc "+lc.typ)
			pendingImports[lc.path] = lc.alias
		}
		for _, p := range op.Params {
			params = append(params, p.Name+" "+paramType(p))
		}
		fmt.Fprintf(buf, "\t%s(%s)%s\n", op.Name, strings.Join(params, ", "), returnClause(op))
	}
	buf.WriteString("}\n\n")
}

// paramType renders a parameter's Go type, pointer-wrapped when the param is a
// load-bearing nullable pointer.
func paramType(p Param) string {
	t := goType(p.Schema)
	if p.Pointer {
		t = "*" + t
	}
	return t
}

// returnClause renders an operation's return signature.
func returnClause(op Operation) string {
	switch {
	case op.Result != nil && op.Error:
		return " (" + goType(op.Result) + ", error)"
	case op.Result != nil:
		return " " + goType(op.Result)
	case op.Error:
		return " error"
	default:
		return ""
	}
}
