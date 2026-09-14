package methodassets

import "testing"

// TestInStepScope pins, path by path, which files ClaudeFilesFor seats for one
// step. TestClaudeFilesForIsScoped proves the property over the real asset
// tree. This test covers the edges that tree does not happen to exercise: a
// step with no agent, a near-miss file name, and a skill's reference files.
func TestInStepScope(t *testing.T) {
	m := StepManifest{Command: "mission-draft", Agent: "system-architect"}
	noAgent := StepManifest{Command: "mission-draft"}
	skills := map[string]bool{"the-method-doctrine": true}

	cases := []struct {
		name string
		path string
		m    StepManifest
		want bool
	}{
		{"its own command", ".claude/commands/mission-draft.md", m, true},
		{"another command", ".claude/commands/glossary-draft.md", m, false},
		{"a near-miss of its own command", ".claude/commands/mission-draft.md.bak", m, false},
		{"its own charter", ".claude/agents/system-architect.md", m, true},
		{"another charter", ".claude/agents/product-manager.md", m, false},
		{"no agent seats no charter", ".claude/agents/.md", noAgent, false},
		{"a skill in its closure", ".claude/skills/the-method-doctrine/SKILL.md", m, true},
		{"a reference file of a closure skill", ".claude/skills/the-method-doctrine/refs/appendix.md", m, true},
		{"a skill outside its closure", ".claude/skills/the-method-layers/SKILL.md", m, false},
		{"a shared file outside commands, agents and skills", ".claude/settings.json", m, true},
		{"a shared doc", ".claude/docs/glossary.md", noAgent, true},
	}
	for _, tc := range cases {
		if got := inStepScope(tc.path, tc.m, skills); got != tc.want {
			t.Errorf("%s: inStepScope(%q) = %v, want %v", tc.name, tc.path, got, tc.want)
		}
	}
}
