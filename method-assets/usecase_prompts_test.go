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

func TestCoreUseCasesPrompts_CiteTheActorDoorRules(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, wants := range map[string][]string{
		".claude/commands/core-use-cases-critique.md": {"UC-ACTOR-DOOR", "UC-LANE-RESOLVES", "linkedActorId"},
		".claude/commands/core-use-cases-draft.md": {
			"UC-ACTOR-DOOR", "UC-LANE-RESOLVES", "UC-PATH-OUTCOME", "UC-START-NOT-ACTION", "linkedActorId",
		},
		".claude/skills/the-method-core-use-cases/SKILL.md": {
			"UC-ACTOR-DOOR", "UC-LANE-RESOLVES", "UC-PATH-OUTCOME", "UC-START-NOT-ACTION", "linkedActorId",
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
