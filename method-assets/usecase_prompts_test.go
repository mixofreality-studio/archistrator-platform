package methodassets

import (
	"regexp"
	"strings"
	"testing"
)

// RUN-024 committed 101 activity nodes with linkedActorId null: the prompts told the agent to
// set `linkedActor` (the schema field is linkedActorId) as "an actor's ROLE name", so an agent
// following them had no way to write the link the rules read. These tests hold the prompts to the
// schema's vocabulary and to citing the rules that now refuse an unlinked diagram.

// linkedActorBare is the field name without its Id suffix: `linkedActor` followed by anything but
// a further identifier character. `linkedActorId` does not match.
var linkedActorBare = regexp.MustCompile(`linkedActor\b`)

func TestPrompts_UseTheSchemaFieldLinkedActorID(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		if loc := linkedActorBare.FindIndex(body); loc != nil {
			from := max(loc[0]-40, 0)
			to := min(loc[1]+40, len(body))
			t.Errorf("%s says `linkedActor`; the schema field is `linkedActorId` (an actor's id): ...%s...", path, body[from:to])
		}
	}
}

// The core-use-cases critique runs at slot 4, where no Client and no contract exists: the coverage
// table's Door column reads NONE on every row, and the derivation reports SCN-ACTOR-PATH-CLIENT on
// every path an actor acts on. A critique told to send that back sends back every honest draft
// (RUN-025's class of defect, in prose). So no line of it may make a NONE Door a REVISE, it must not
// claim the contract-surface rules fire when the draft is recorded, and it judges the actor's door
// by what slot 4 has: UC-ACTOR-DOOR, UC-PATH-OUTCOME and UC-START-NOT-ACTION. Which rules a REVISE
// line may cite at all is held against methodcheck's prerequisite registry in
// framework-go-conformance (critique_prerequisites_test.go).
func TestCoreUseCasesCritique_JudgesOnlyWhatExistsAtSlot4(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	const critique = ".claude/commands/core-use-cases-critique.md"
	body := string(files[critique])
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Door") && strings.Contains(line, "NONE") && strings.Contains(line, "REVISE") {
			t.Errorf("%s:%d makes a Door of NONE a REVISE; no Client exists at slot 4: %s", critique, i+1, line)
		}
	}
	if regexp.MustCompile(`(?i)fire at record time`).MatchString(body) {
		t.Errorf("%s says the rules fire at record time; the contract-surface rules wait for the architecture and the contracts", critique)
	}
	for _, want := range []string{"UC-ACTOR-DOOR", "UC-PATH-OUTCOME", "UC-START-NOT-ACTION", "judged at the architecture"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not say %q", critique, want)
		}
	}
}

func TestCoreUseCasesPrompts_CiteTheActorDoorRules(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, wants := range map[string][]string{
		".claude/commands/core-use-cases-critique.md": {"UC-ACTOR-DOOR", "UC-ACTOR-UNSERVED", "UC-LANE-RESOLVES", "UC-PATH-OUTCOME-DISTINCT", "linkedActorId", "kind: agent", "kind: system", "owes only its outcome"},
		".claude/commands/core-use-cases-draft.md": {
			"UC-ACTOR-DOOR", "UC-ACTOR-UNSERVED", "UC-LANE-RESOLVES", "UC-PATH-OUTCOME", "UC-PATH-OUTCOME-DISTINCT", "UC-START-NOT-ACTION", "linkedActorId",
			"kind: agent", "kind: system", "owes only its outcome",
		},
		".claude/skills/the-method-core-use-cases/SKILL.md": {
			"UC-ACTOR-DOOR", "UC-LANE-RESOLVES", "UC-PATH-OUTCOME", "UC-PATH-OUTCOME-DISTINCT", "UC-START-NOT-ACTION", "linkedActorId",
		},
	} {
		body := string(files[path])
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not cite %q", path, want)
			}
		}
	}
}

// RUN-024 also lost seven putDraftModel calls to a skill that described use cases, actors and
// nodes as identified by name with no ids, `ref` slugs on nodes and `variationOf` by name, when the
// schema keys all of them by `id`. These phrases are the stale vocabulary; the strict decode of the
// skill's example JSON lives in framework-go-conformance (usecase_wire_prompts_test.go).
var staleUseCaseVocabulary = []*regexp.Regexp{
	regexp.MustCompile(`(?i)identity by name \(no ids\)`),
	regexp.MustCompile(`(?i)you (do not|never) emit any (opaque )?id`),
	regexp.MustCompile(`"ref":`),
	regexp.MustCompile(`\{ref,`),
	regexp.MustCompile("(?i)`ref` slug"),
	regexp.MustCompile("(?i)variationOf` by its name"),
	regexp.MustCompile("(?i)variationOf` that does not resolve to a core use case's name"),
	regexp.MustCompile("(?i)every use case and actor is identified by its human-readable name"),
	regexp.MustCompile("(?i)use case names must be unique"),
	regexp.MustCompile("(?i)set `useCase` to that use case's name"),
	regexp.MustCompile("(?i)`activityNode` names a node"),
	regexp.MustCompile("(?i)component names or an actor role"),
}

func TestPrompts_DoNotDescribeUseCasesByNameWhenTheSchemaKeysThemByID(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range files {
		for _, re := range staleUseCaseVocabulary {
			if loc := re.FindIndex(body); loc != nil {
				from := max(loc[0]-40, 0)
				to := min(loc[1]+40, len(body))
				t.Errorf("%s matches stale wire vocabulary %s: ...%s...", path, re, body[from:to])
			}
		}
	}
}

func TestCoreUseCasesSkill_TeachesTheIDKeyedWireShape(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	body := string(files[".claude/skills/the-method-core-use-cases/SKILL.md"])
	for _, want := range []string{"IDENTITY BY ID", "`variationOf`", "`essenceRationale`", "`linkedActorId`", "`decidedBy`", "`rejectionReason`"} {
		if !strings.Contains(body, want) {
			t.Errorf("the core-use-cases skill does not mention %s", want)
		}
	}
}
