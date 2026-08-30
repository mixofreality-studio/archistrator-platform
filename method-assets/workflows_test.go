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

// TestWorkflowTemplatesScopeThePromptSurface pins the CLOUD rail's half of the
// per-step scoping. The local executor is covered by its own tests; without
// this, the two rails could silently diverge and the same step would carry a
// different context in CI than it does locally — the exact defect that makes a
// benchmark result untransferable to production.
func TestWorkflowTemplatesScopeThePromptSurface(t *testing.T) {
	for _, name := range []string{"aiarch-design.yml.tmpl", "aiarch-construct.yml.tmpl"} {
		body, err := assetsFS.ReadFile("assets/workflows/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(body)

		// The seat step must scope to this step, not render all 59 commands.
		if !strings.Contains(s, `seat-assets --dest . --command "${{ inputs.command }}"`) {
			t.Errorf("%s: seat step must scope the surface with --command", name)
		}
		// The MCP server must know the step, or it registers the full tool catalog.
		if !strings.Contains(s, `"AIARCH_COMMAND": "${{ inputs.command }}"`) {
			t.Errorf("%s: MCP config must stamp AIARCH_COMMAND (the step manifest key)", name)
		}
		// Prompt-surface isolation: without this the runner's ambient ~/.claude
		// (plugins, skills, agents) loads into the dispatched agent.
		if !strings.Contains(s, "--setting-sources project") {
			t.Errorf("%s: claude_args must pass --setting-sources project", name)
		}
		if !strings.Contains(s, "--strict-mcp-config") {
			t.Errorf("%s: claude_args must keep --strict-mcp-config", name)
		}
		// The deny list must come from the binary (one source of truth), never
		// be restated in YAML.
		if !strings.Contains(s, "step-tools --command") {
			t.Errorf("%s: must resolve --disallowedTools via `aiarch-state-mcp step-tools`", name)
		}
		if !strings.Contains(s, "${{ steps.steptools.outputs.disallowed }}") {
			t.Errorf("%s: claude_args must consume the resolved deny list", name)
		}
	}
}
