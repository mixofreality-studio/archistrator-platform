package methodassets

import (
	"strings"
	"testing"
)

// wantWrites: the exact aiarch-state WRITE verbs each role may hold (spec §4).
var wantWrites = map[string][]string{
	"system-architect": {"putDraftModel", "setCritiqueVerdict", "recordServiceContract", "recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"product-manager":  {"setCritiqueVerdict", "respondToReviewComment", "publishDraft"},
	"project-manager":  {"putDraftModel", "recordPhaseArtifact", "publishDraft"},
	"senior-developer": {"recordServiceContract", "recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"junior-developer": {"recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"ui-designer":      {"recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"ux-reviewer":      {"respondToReviewComment"},
	"test-engineer":    {"recordTestingState", "recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"software-tester":  {"recordTestingState", "recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
	"qa-engineer":      {"recordTestingState", "recordPhaseArtifact", "publishDraft", "respondToReviewComment"},
}

var allWrites = []string{
	"putDraftModel", "setCritiqueVerdict", "recordServiceContract",
	"recordPhaseArtifact", "recordTestingState", "publishDraft",
	"respondToReviewComment",
}

// wantSharedReads: the aiarch-state READ verbs open to every role (spec §4 — reads are shared).
var wantSharedReads = []string{
	"getCommittedSlot", "getDraftSlot", "getReviewThread", "getCritique",
	"listResearchSources", "getResearchSource", "projectStateReadProject",
}

func TestAgentSharedReads(t *testing.T) {
	files, _ := ClaudeFiles()
	for role := range wantWrites {
		body := string(files[".claude/agents/"+role+".md"])
		fm := body[:strings.Index(body, "\n---")]
		for _, r := range wantSharedReads {
			if !strings.Contains(fm, "mcp__aiarch-state__"+r) {
				t.Errorf("%s: missing shared read %s", role, r)
			}
		}
	}
}

func TestAgentToolScoping(t *testing.T) {
	files, _ := ClaudeFiles()
	for role, wants := range wantWrites {
		body := string(files[".claude/agents/"+role+".md"])
		fm := body[:strings.Index(body, "\n---")] // frontmatter block
		if !strings.Contains(fm, "tools:") {
			t.Errorf("%s: no tools: frontmatter", role)
			continue
		}
		allowed := map[string]bool{}
		for _, w := range wants {
			allowed[w] = true
			if !strings.Contains(fm, "mcp__aiarch-state__"+w) {
				t.Errorf("%s: missing sanctioned write %s", role, w)
			}
		}
		for _, w := range allWrites {
			if !allowed[w] && strings.Contains(fm, "mcp__aiarch-state__"+w) {
				t.Errorf("%s: holds UNSANCTIONED write %s", role, w)
			}
		}
	}
	// ux-reviewer reviews, never amends: no Edit/Write built-ins.
	fm := string(files[".claude/agents/ux-reviewer.md"])
	fm = fm[:strings.Index(fm, "\n---")]
	for _, banned := range []string{"Edit", "Write"} {
		for _, line := range strings.Split(fm, "\n") {
			l := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if l == banned {
				t.Errorf("ux-reviewer: banned built-in %s", banned)
			}
		}
	}
}
