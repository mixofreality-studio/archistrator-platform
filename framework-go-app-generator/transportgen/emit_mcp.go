package transportgen

import (
	"bytes"
	"fmt"
	"strings"

	httpgen "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// emitManagerMCP emits mcp_<mgr>.gen.go: one typed method on the shared
// MCPClient per operation (tool name lowerFirst(ManagerBase)+Op), plus an input
// struct carrying ALL of the op's params as JSON tool arguments.
func emitManagerMCP(info *mgrInfo, cfg Config) ([]byte, error) {
	var body bytes.Buffer
	for _, op := range info.doc.Interface.Operations {
		writeMCPMethod(&body, info, op, info.plans[op.Name], cfg)
	}

	var out bytes.Buffer
	out.WriteString(genHeader)
	fmt.Fprintf(&out, "package %s\n\n", cfg.PackageName)
	writeImports(&out, []string{"context"})
	out.Write(body.Bytes())
	return formatFile(out.String())
}

func writeMCPMethod(b *bytes.Buffer, info *mgrInfo, op projectmodel.Operation, plan httpgen.OpPlan, cfg Config) {
	method := info.base + op.Name
	tool := info.toolPref + op.Name
	inType := method + "Input"
	pathParams := pathParamSet(plan)

	// Input struct: every param is a JSON tool argument (tag = wire param name).
	// A param that plan resolves as an HTTP path param is ALWAYS a value scalar
	// here too — MCP has no path/query/body distinction, but the SDK keeps
	// HTTPClient and MCPClient signatures identical per op (see transportgen
	// migration notes), and the value is never actually optional on the wire
	// (the "-" placeholder convention supplies a concrete value, never nil).
	fmt.Fprintf(b, "// %s is the MCP tool-call argument object for %s.\n", inType, tool)
	fmt.Fprintf(b, "type %s struct {\n", inType)
	for _, p := range op.Params {
		ptr := p.Pointer && !pathParams[p.Name]
		fmt.Fprintf(b, "\t%s %s `json:%q`\n", upperFirst(p.Name),
			goType(p.Schema, ptr, info.rename, cfg.UUIDAsString), jsonTag(p.Name, ptr))
	}
	b.WriteString("}\n\n")

	sig := []string{"ctx context.Context"}
	for _, p := range op.Params {
		ptr := p.Pointer && !pathParams[p.Name]
		sig = append(sig, p.Name+" "+goType(p.Schema, ptr, info.rename, cfg.UUIDAsString))
	}
	resultType := goType(op.Result, false, info.rename, cfg.UUIDAsString)
	inLit := inputLiteral(inType, op)

	fmt.Fprintf(b, "// %s calls the %s tool on the %s manager over MCP.\n", method, tool, info.base)
	if plan.HasResult {
		fmt.Fprintf(b, "func (c *MCPClient) %s(%s) (%s, error) {\n", method, strings.Join(sig, ", "), resultType)
		fmt.Fprintf(b, "\treturn mcpCallResult[%s](c, ctx, %q, %s)\n", resultType, tool, inLit)
	} else {
		fmt.Fprintf(b, "func (c *MCPClient) %s(%s) error {\n", method, strings.Join(sig, ", "))
		fmt.Fprintf(b, "\treturn c.callTool(ctx, %q, %s, nil)\n", tool, inLit)
	}
	b.WriteString("}\n\n")
}

// inputLiteral renders the MCP input composite literal, filling one field per
// op param.
func inputLiteral(inType string, op projectmodel.Operation) string {
	if len(op.Params) == 0 {
		return inType + "{}"
	}
	fields := make([]string, 0, len(op.Params))
	for _, p := range op.Params {
		fields = append(fields, upperFirst(p.Name)+": "+p.Name)
	}
	return inType + "{" + strings.Join(fields, ", ") + "}"
}
