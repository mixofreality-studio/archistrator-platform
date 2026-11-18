package conformance

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// A critic's send-back is judged at its slot. RUN-025's defect class, in prose: the core-use-cases
// critique told the PM to REVISE a path whose Door was NONE and cited SCN-ACTOR-PATH-CLIENT and
// TP-CLIENT-CONTRACTED as firing when the draft is recorded, while at slot 4 no Client and no
// contract exists, so every honest draft would have been sent back. methodcheck's prerequisite
// registry already says which artifacts each rule needs recorded; a REVISE line of a critique may
// cite a rule only when those are recorded by the slot the critique reviews.

// critiqueSlots is each design-slot critique and the artifacts recorded when it runs: its own
// slot's draft and every committed slot before it.
var critiqueSlots = func() map[string][]methodcheck.Prerequisite {
	order := []struct {
		command string
		slot    methodcheck.Prerequisite
	}{
		{"mission-critique", methodcheck.RequiresMission{}},
		{"glossary-critique", methodcheck.RequiresGlossary{}},
		{"scrubbed-requirements-critique", methodcheck.RequiresScrubbedRequirements{}},
		{"volatilities-critique", methodcheck.RequiresVolatilities{}},
		{"core-use-cases-critique", methodcheck.RequiresCoreUseCases{}},
		{"system-critique", methodcheck.RequiresSystem{}},
	}
	out := map[string][]methodcheck.Prerequisite{}
	var recorded []methodcheck.Prerequisite
	for _, o := range order {
		recorded = append(recorded, o.slot)
		out[".claude/commands/"+o.command+".md"] = append([]methodcheck.Prerequisite(nil), recorded...)
	}
	return out
}()

// ruleIDToken is a rule id as the prompts cite it (UC-ACTOR-DOOR, SCN-UNADDRESSED, CC-COVERAGE).
var ruleIDToken = regexp.MustCompile(`\b[A-Z]{2,6}(?:-[A-Z0-9]+)+\b`)

func TestCritiques_RequestARevisionOnlyOnRulesTheirSlotCanJudge(t *testing.T) {
	files, err := methodassets.ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for path, recorded := range critiqueSlots {
		body, ok := files[path]
		if !ok {
			t.Errorf("%s is missing from the embedded .claude tree", path)
			continue
		}
		have := map[string]bool{}
		for _, pr := range recorded {
			have[methodcheck.PrerequisiteName(pr)] = true
		}
		cited := 0
		for i, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, "REVISE") {
				continue
			}
			for _, id := range ruleIDToken.FindAllString(line, -1) {
				prs, known := methodcheck.RulePrerequisites(methodcheck.RuleID(id))
				if !known {
					continue
				}
				cited++
				for _, pr := range prs {
					if name := methodcheck.PrerequisiteName(pr); !have[name] {
						t.Errorf("%s:%d asks for a REVISE on %s, which needs %s recorded; it is not at this slot", path, i+1, id, name)
					}
				}
			}
		}
		if path == ".claude/commands/core-use-cases-critique.md" && cited == 0 {
			t.Errorf("%s cites no methodcheck rule on a REVISE line: the check is vacuous", path)
		}
	}
}
