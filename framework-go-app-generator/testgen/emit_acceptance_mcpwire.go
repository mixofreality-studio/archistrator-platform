package testgen

import "fmt"

// emit_acceptance_mcpwire.go is the MCP dialect of the acceptance emitters: an MCP client's step is
// one tool call over the wire, in a Go test through a scenariohost.MCPSession on the launched app
// and in a Playwright spec through mcpCall, one /mcp session per (app, actor) in both. The tool is
// the generated one: projectmodel.LowerFirst of the client's operation name (toolName).

// mcpSessionLine opens a session on the launched app as who.
func mcpSessionLine(s accSession, who string) string {
	return fmt.Sprintf("%s := scenariohost.MCPSession(t, app, %s)", s.variable(), who)
}
