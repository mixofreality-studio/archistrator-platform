package methodassets

import (
	"regexp"
	"strings"
	"testing"
)

// The write verbs a command may INSTRUCT an agent to perform. Must match the
// keys of the frontmatter write matrix (allWrites in frontmatter_test.go).
var commandWriteVerbs = map[string]bool{
	"putDraftModel":          true,
	"setCritiqueVerdict":     true,
	"recordServiceContract":  true,
	"recordPhaseArtifact":    true,
	"recordTestingState":     true,
	"publishDraft":           true,
	"respondToReviewComment": true,
}

var (
	numberedStep = regexp.MustCompile(`^\s*\d+\.\s`)
	backtickTok  = regexp.MustCompile("`([^`]+)`")
	negationCue  = regexp.MustCompile(`(?i)\b(no|not|never|without|cannot|nor)\b|n't`)
)

// knownAgentSet is the set of agent role names (the .claude/agents/*.md stems).
func knownAgentSet(files map[string][]byte) map[string]bool {
	set := map[string]bool{}
	for path := range files {
		if strings.HasPrefix(path, ".claude/agents/") && strings.HasSuffix(path, ".md") {
			name := strings.TrimSuffix(strings.TrimPrefix(path, ".claude/agents/"), ".md")
			set[name] = true
		}
	}
	return set
}

// agentsForCommand resolves the role name(s) named on the command's
// "**Agent + skills.**" line. Commands may name several roles (e.g. "or"); a
// role backticked in a reviewer list still counts — the assertion below is a
// lower bound ("at least one named agent holds the write"). Returns nil when
// the file has no agent line (orchestrator commands that only dispatch).
func agentsForCommand(body string, known map[string]bool) []string {
	var line string
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "Agent + skills") {
			line = l
			break
		}
	}
	if line == "" {
		return nil
	}
	var roles []string
	seen := map[string]bool{}
	for _, m := range backtickTok.FindAllStringSubmatch(line, -1) {
		tok := m[1]
		if known[tok] && !seen[tok] {
			seen[tok] = true
			roles = append(roles, tok)
		}
	}
	return roles
}

// instructedWrites extracts the write verbs a command instructs, scanning only
// numbered instruction steps (the shared aiarch-state tool-menu blockquote and
// prose are not instructions), and skipping any occurrence sitting in a
// negated clause (e.g. "the PM never calls `putDraftModel`").
func instructedWrites(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if !numberedStep.MatchString(line) {
			continue
		}
		for verb := range commandWriteVerbs {
			for _, form := range []string{"`" + verb + "`", "mcp__aiarch-state__" + verb} {
				idx := strings.Index(line, form)
				for idx >= 0 {
					start := idx - 24
					if start < 0 {
						start = 0
					}
					if !negationCue.MatchString(line[start:idx]) {
						out[verb] = true
					}
					next := strings.Index(line[idx+len(form):], form)
					if next < 0 {
						break
					}
					idx = idx + len(form) + next
				}
			}
		}
	}
	return out
}

// TestCommandWritesAreGranted is the cross-file invariant: every write verb a
// command instructs must be granted (in tools: frontmatter) to at least one of
// the roles that command names. This is the guard behind the construction-write
// grant fix — with recordServiceContract/recordPhaseArtifact removed from the
// agents, the 18 construction commands that instruct them go RED here.
func TestCommandWritesAreGranted(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}
	known := knownAgentSet(files)

	// Sanity: the verb vocabulary here matches the frontmatter matrix.
	for _, w := range allWrites {
		if !commandWriteVerbs[w] {
			t.Fatalf("write verb %q in allWrites but not in commandWriteVerbs", w)
		}
	}

	for path, raw := range files {
		if !strings.HasPrefix(path, ".claude/commands/") {
			continue
		}
		body := string(raw)
		roles := agentsForCommand(body, known)
		writes := instructedWrites(body)
		if len(writes) == 0 {
			continue
		}
		if len(roles) == 0 {
			t.Errorf("%s: instructs writes %v but names no known agent on its Agent line", path, keys(writes))
			continue
		}
		granted := map[string]bool{}
		for _, r := range roles {
			for _, w := range wantWrites[r] {
				granted[w] = true
			}
		}
		for w := range writes {
			if !granted[w] {
				t.Errorf("%s: instructs %q but none of its named agents %v is granted it", path, w, roles)
			}
		}
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
