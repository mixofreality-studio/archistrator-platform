package methodassets

import (
	"strings"
	"testing"
)

func TestWorkflowTemplates(t *testing.T) {
	for _, name := range []string{"aiarch-design.yml.tmpl", "aiarch-construct.yml.tmpl"} {
		body, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(body)
		if strings.Contains(s, "design_prompt") {
			t.Errorf("%s: raw design_prompt input must be gone (thin dispatch)", name)
		}
		if !strings.Contains(s, "/${{ inputs.command }}") {
			t.Errorf("%s: prompt must be the thin slash-command invocation", name)
		}
		if !strings.Contains(s, "[[.AppSlug]]") {
			t.Errorf("%s: allowed_bots must be templated on [[.AppSlug]]", name)
		}
	}
	// Construct must NOT build the MCP from source (app repos have no server source).
	c, _ := assetsFS.ReadFile("assets/workflows/aiarch-construct.yml.tmpl")
	if strings.Contains(string(c), "go build ./cmd/aiarch-state-mcp") {
		t.Error("construct template still builds MCP from checked-out source; must go install [[.StateMcpModulePath]]@[[.StateMcpModuleVersion]]")
	}
}
