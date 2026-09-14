package main

import (
	"reflect"
	"sort"
	"testing"
)

// main_test.go pins the generator's two filters: which charter tools a step
// keeps, and which skill links count as edges. The package's drift gate
// (stepmanifest_test.go, TestStepManifestRegenerationIsNoOp) only proves that
// the CURRENT asset set regenerates unchanged. These tests cover the rules for
// inputs the current assets happen not to exercise.

const mcp = "mcp__aiarch-state__"

func TestToolAllowed(t *testing.T) {
	cases := []struct {
		name            string
		tool, mode      string
		isProjectDesign bool
		want            bool
	}{
		{"a built-in always passes", "Read", modeConstruct, false, true},
		{"a non-aiarch MCP tool is treated as a built-in", "mcp__other__thing", modeDraft, false, true},
		{"a composed verb passes in a mode it serves", mcp + "putDraftModel", modeDraft, false, true},
		{"a composed verb is dropped outside its modes", mcp + "putDraftModel", modeCritique, false, false},
		{"a composed verb ignores the project-design flag", mcp + "putDraftModel", modeCritique, true, false},
		{"an ambient read is served in construct", mcp + "getCommittedSlot", modeConstruct, false, true},
		{"a Kind-resolving read is not served in construct", mcp + "getDraftSlot", modeConstruct, false, false},
		{"a project-design verb passes on a project-design step", mcp + "estimationComputeNetwork", modeDraft, true, true},
		{"a project-design verb is dropped elsewhere", mcp + "estimationComputeNetwork", modeDraft, false, false},
		{"any other raw read passes", mcp + "projectStateReadProject", modeCritique, false, true},
		{"any other raw read passes on a project-design step", mcp + "projectStateReadProject", modeDraft, true, true},
	}
	for _, tc := range cases {
		if got := toolAllowed(tc.tool, tc.mode, tc.isProjectDesign); got != tc.want {
			t.Errorf("%s: toolAllowed(%q, %q, %v) = %v, want %v", tc.name, tc.tool, tc.mode, tc.isProjectDesign, got, tc.want)
		}
	}
}

func TestAllowedTools_FiltersAndSorts(t *testing.T) {
	charter := []string{"Write", mcp + "estimationDerivePlan", mcp + "setCritiqueVerdict", "Bash", mcp + "putDraftModel", mcp + "getCommittedSlot"}
	got := allowedTools(charter, modeCritique, false)
	want := []string{"Bash", "Write", mcp + "getCommittedSlot", mcp + "setCritiqueVerdict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowedTools = %v, want %v", got, want)
	}
	if got := allowedTools(nil, modeDraft, false); got == nil || len(got) != 0 {
		t.Fatalf("an empty charter must yield an empty, non-nil list; got %#v", got)
	}
}

func TestSkillLinks_KeepsSeatedNonSelfTargetsInBodyOrder(t *testing.T) {
	known := map[string]bool{"a": true, "b": true, "c": true}
	got := skillLinks("a", []byte("[[c]] then [[b]], never [[a]] or [[ghost]], and [[c]] again"), known)
	if want := []string{"c", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skillLinks = %v, want %v", got, want)
	}
	if got := skillLinks("a", []byte("no links"), known); got != nil {
		t.Fatalf("a body with no links yields nil, got %#v", got)
	}
}

func TestParseSkillGraph(t *testing.T) {
	files := map[string][]byte{
		".claude/skills/a/SKILL.md":   []byte("see [[b]] and [[a]] and [[ghost]] and [[c]]"),
		".claude/skills/a/ref.md":     []byte("also [[c]]"), // a skill's reference files link too
		".claude/skills/b/SKILL.md":   []byte("no links at all"),
		".claude/skills/c/SKILL.md":   []byte("[[b]]"),
		".claude/commands/x.md":       []byte("[[a]]"), // a command is not a skill: no edges
		".claude/skills/loose.md":     []byte("[[a]]"), // not under a skill directory
		".claude/agents/architect.md": []byte("[[c]]"),
	}
	got := parseSkillGraph(files)
	for _, targets := range got {
		sort.Strings(targets) // files are visited in map order; only membership is the contract
	}
	want := map[string][]string{
		"a": {"b", "c", "c"},
		"c": {"b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSkillGraph = %v, want %v (a skill with no links must have no entry)", got, want)
	}
}
