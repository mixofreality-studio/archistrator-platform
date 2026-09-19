package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
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

// ---------------------------------------------------------------------------
// the generator's own tables, read against each other
// ---------------------------------------------------------------------------
//
// The drift gate (stepmanifest_test.go, TestStepManifestRegenerationIsNoOp)
// compares the committed stepmanifest.gen.go against this generator's output, so
// it catches every generator change that MOVES the output. It cannot catch a
// generator change that moves nothing — a verb-name typo in a table no charter
// reaches is invisible to it (archistrator B1 re-review, the one surviving
// mutant: renaming composedVerbModes' "getOperatorNotes" key back to the old
// snake_case name changed no byte of the manifest, because modeImplicitVerbs
// re-adds the verb to every construct step anyway). These two read the tables
// against each other and against the charters, so a name that exists in neither
// is a dead entry and a failure.

// TestModeImplicitVerbsAreComposedVerbsServingThatMode pins the two tables to each
// other. A mode-implicit verb is granted to EVERY step of its mode, so it must be a
// composed verb the registry serves in that mode — otherwise the generator grants a
// verb its own narrowing table says does not exist there, and the two halves of a
// rename have silently come apart.
func TestModeImplicitVerbsAreComposedVerbsServingThatMode(t *testing.T) {
	if len(modeImplicitVerbs) == 0 {
		t.Fatal("modeImplicitVerbs is empty — this gate would pass vacuously")
	}
	for mode, verbs := range modeImplicitVerbs {
		for _, v := range verbs {
			modes, ok := composedVerbModes[v]
			if !ok {
				t.Errorf("modeImplicitVerbs[%q] grants %q, which composedVerbModes does not name — "+
					"one of the two spellings is stale (known names: %s)", mode, v, strings.Join(composedVerbNames(), ", "))
				continue
			}
			if !contains(modes, mode) {
				t.Errorf("modeImplicitVerbs[%q] grants %q to every %[1]s step, but composedVerbModes serves it only in %v",
					mode, v, modes)
			}
		}
	}
}

// TestEveryComposedVerbIsReachable fails on a DEAD entry in composedVerbModes: a verb
// no agent charter declares and no mode implies is narrowing nothing, which is exactly
// what a renamed key looks like.
func TestEveryComposedVerbIsReachable(t *testing.T) {
	files, err := methodassets.ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, tools := range parseCharters(files) {
		for _, tool := range tools {
			if verb, isMCP := strings.CutPrefix(tool, mcp); isMCP {
				declared[verb] = true
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no charter declares an aiarch-state verb — this gate would pass vacuously")
	}
	implicit := map[string]bool{}
	for _, verbs := range modeImplicitVerbs {
		for _, v := range verbs {
			implicit[v] = true
		}
	}
	for _, verb := range composedVerbNames() {
		if !declared[verb] && !implicit[verb] {
			t.Errorf("composedVerbModes names %q, but no agent charter declares %s%[1]s and no mode "+
				"implies it — the entry narrows nothing (a stale spelling, or a verb that left the charters)", verb, mcp)
		}
	}
}

// composedVerbNames returns composedVerbModes' keys, sorted.
func composedVerbNames() []string {
	names := make([]string, 0, len(composedVerbModes))
	for v := range composedVerbModes {
		names = append(names, v)
	}
	sort.Strings(names)
	return names
}
