// Command gen-stepmanifest regenerates stepmanifest.gen.go — the per-step
// prompt-surface manifest — from the embedded .claude assets.
//
// It derives, never invents. Every field traces to a source that already
// exists and is already asserted by another test in this package:
//
//	Agent  <- the command's "**Agent + skills.**" line (commandwrites_test.go
//	          parses the same line for the write-verb gate)
//	Skills <- the [[wikilinks]] on that line, transitively closed over
//	          skill-to-skill links so no seated skill points at an unseated one
//	Tools  <- the agent charter's `tools:` frontmatter (frontmatter_test.go
//	          asserts its read/write membership), narrowed to the step's job
//	          mode and to whether the step is a project-design step
//
// Run from the module root:
//
//	go run ./cmd/gen-stepmanifest
//
// stepmanifest_test.go fails if the committed file differs from this output.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strings"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

const outPath = "stepmanifest.gen.go"

// ---------------------------------------------------------------------------
// job modes
// ---------------------------------------------------------------------------

const (
	modeDraft     = "draft"
	modeCritique  = "critique"
	modeAnswer    = "answer"
	modeConstruct = "construct"
)

// orchestrationCommands are human-facing or server-assembled commands that are
// NEVER dispatched to an agent, so they get no manifest: /system-design and
// /project-design are operator entry points, /add-use-case and
// /implement-project are driven interactively, and /sdp-review is assembled
// server-side from committed slots.
var orchestrationCommands = map[string]bool{
	"system-design":     true,
	"project-design":    true,
	"add-use-case":      true,
	"implement-project": true,
	"sdp-review":        true,
}

// modeFor derives a command's job mode from its slug — the same suffix
// convention projectstateaccess.go's CommandFor/DesignCommandFor emit.
func modeFor(slug string) string {
	switch {
	case strings.HasPrefix(slug, "design-answer"):
		return modeAnswer
	case strings.HasSuffix(slug, "-critique"):
		return modeCritique
	case strings.HasSuffix(slug, "-draft"):
		return modeDraft
	default:
		// The construction phase commands: <type>-requirements,
		// -detailed-design, -test-plan, -construction, -integration.
		return modeConstruct
	}
}

// composedVerbModes mirrors cmd/aiarch-state-mcp's composedVerbs registry: the
// job modes in which each hand-curated verb is registered at all. A manifest
// may never grant a verb its mode cannot serve. Keep in sync with tools.go —
// stepmanifest_test.go asserts every name here is a real registered verb.
var composedVerbModes = map[string][]string{
	// ambient-kind-independent reads: legal in every mode, construct included
	"listResearchSources": {modeDraft, modeCritique, modeAnswer, modeConstruct},
	"getResearchSource":   {modeDraft, modeCritique, modeAnswer, modeConstruct},
	"getCommittedSlot":    {modeDraft, modeCritique, modeAnswer, modeConstruct},
	"publishDraft":        {modeDraft, modeCritique, modeAnswer, modeConstruct},
	// reads that resolve the ambient artifact Kind — construct sets no Kind
	"getDraftSlot":    {modeDraft, modeCritique, modeAnswer},
	"getCritique":     {modeDraft, modeCritique, modeAnswer},
	"getReviewThread": {modeDraft, modeCritique, modeAnswer},
	// mode-specific writes
	"setCritiqueVerdict":     {modeCritique},
	"putDraftModel":          {modeDraft},
	"respondToReviewComment": {modeDraft, modeAnswer},
	"recordServiceContract":  {modeConstruct},
	"recordPhaseArtifact":    {modeConstruct},
	"recordTestingState":     {modeConstruct},
	// the construct-mode read of the operator's steer for this attempt
	"get_operator_notes": {modeConstruct},
}

// modeImplicitVerbs are the composed verbs EVERY step of a mode is granted,
// whatever its charter lists. get_operator_notes is the operator's steer for
// the attempt (a send-back, retry or re-queue note): every construct command
// tells the agent to call it first, so every construct step must hold it —
// a step whose charter predates the tool would otherwise have it filtered out
// of its surface (archistrator B1, amendment §C.1 finding H9).
var modeImplicitVerbs = map[string][]string{
	modeConstruct: {"get_operator_notes"},
}

// withModeImplicit adds the mode-implicit verbs to an already-narrowed tool
// list, sorted and without duplicates.
func withModeImplicit(tools []string, mode string) []string {
	for _, v := range modeImplicitVerbs[mode] {
		if name := "mcp__aiarch-state__" + v; !contains(tools, name) {
			tools = append(tools, name)
		}
	}
	sort.Strings(tools)
	return tools
}

// projectDesignVerbs are the raw generated tools that serve Phase 2 (project
// design) ONLY — the estimation engines, the cross-branch project read, and
// the manager-facing intervention/review proposals. This is the founder's
// headline case: a system-design step must not carry the project-design tool
// surface. Granted only to projectDesignCommands.
var projectDesignVerbs = map[string]bool{
	"estimationComputeNetwork":         true,
	"estimationEstimateForOption":      true,
	"estimationDerivePlan":             true,
	"estimationComputeEarnedValue":     true,
	"designSessionReadProjectOnBranch": true,
	"interventionDecideOnVariance":     true,
	"reviewProposeReviews":             true,
}

// projectDesignCommands are the Phase 2 artifact steps — the only steps that
// may hold projectDesignVerbs.
var projectDesignCommands = map[string]bool{
	"planning-assumptions-draft":  true,
	"activity-list-draft":         true,
	"network-draft":               true,
	"normal-solution-draft":       true,
	"subcritical-solution-draft":  true,
	"compressed-solution-draft":   true,
	"decompressed-solution-draft": true,
	"risk-model-draft":            true,
}

// ---------------------------------------------------------------------------
// parsing
// ---------------------------------------------------------------------------

var (
	agentPathRe = regexp.MustCompile(`\.claude/agents/([a-z-]+)\.md`)
	wikilinkRe  = regexp.MustCompile(`\[\[([a-z0-9-]+)\]\]`)
	toolLineRe  = regexp.MustCompile(`^\s+-\s+(\S+)\s*$`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-stepmanifest:", err)
		os.Exit(1)
	}
}

func run() error {
	files, err := methodassets.ClaudeFiles()
	if err != nil {
		return err
	}

	skillLinks := parseSkillGraph(files)
	charters := parseCharters(files)

	manifests := map[string]methodassets.StepManifest{}
	for path, body := range files {
		slug, ok := commandSlug(path)
		if !ok || orchestrationCommands[slug] {
			continue
		}
		m, merr := manifestFor(slug, string(body), skillLinks, charters)
		if merr != nil {
			return merr
		}
		manifests[slug] = m
	}
	if len(manifests) == 0 {
		return fmt.Errorf("no dispatchable commands found in the asset set")
	}

	src, ferr := render(manifests)
	if ferr != nil {
		return ferr
	}
	return os.WriteFile(outPath, src, 0o600)
}

func commandSlug(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, ".claude/commands/")
	if !ok || !strings.HasSuffix(rest, ".md") || strings.Contains(rest, "/") {
		return "", false
	}
	return strings.TrimSuffix(rest, ".md"), true
}

// manifestFor builds one step's manifest from its command body.
func manifestFor(slug, body string, skillLinks map[string][]string, charters map[string][]string) (methodassets.StepManifest, error) {
	line := agentSkillsLine(body)
	if line == "" {
		return methodassets.StepManifest{}, fmt.Errorf("command %q has no %q line (add one, or list it in orchestrationCommands)", slug, "**Agent + skills.**")
	}

	agent := ""
	if m := agentPathRe.FindStringSubmatch(line); m != nil {
		agent = m[1]
	}
	if agent == "" {
		return methodassets.StepManifest{}, fmt.Errorf("command %q names no agent charter on its Agent + skills line", slug)
	}

	seeds := []string{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(line, -1) {
		seeds = append(seeds, m[1])
	}
	skills := closeOver(seeds, skillLinks)

	charter, ok := charters[agent]
	if !ok {
		return methodassets.StepManifest{}, fmt.Errorf("command %q names agent %q, which has no charter", slug, agent)
	}

	mode := modeFor(slug)
	tools := withModeImplicit(allowedTools(charter, mode, projectDesignCommands[slug]), mode)

	return methodassets.StepManifest{
		Command: slug,
		Agent:   agent,
		Skills:  skills,
		Tools:   tools,
		Mode:    mode,
	}, nil
}

// agentSkillsLine returns the command's "**Agent + skills.**" line.
func agentSkillsLine(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "Agent + skills") {
			return l
		}
	}
	return ""
}

// allowedTools narrows a charter's declared tools to what this step may hold:
// built-ins pass through; a composed verb passes only if its registry serves
// this mode; a project-design verb passes only for a project-design step; any
// other raw read passes through (they are side-effect-free and cheap).
func allowedTools(charter []string, mode string, isProjectDesign bool) []string {
	out := []string{}
	for _, t := range charter {
		verb, isMCP := strings.CutPrefix(t, "mcp__aiarch-state__")
		if !isMCP {
			out = append(out, t)
			continue
		}
		if modes, composed := composedVerbModes[verb]; composed {
			if contains(modes, mode) {
				out = append(out, t)
			}
			continue
		}
		if projectDesignVerbs[verb] && !isProjectDesign {
			continue
		}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// parseSkillGraph maps each skill name to the skill names its body links to.
func parseSkillGraph(files map[string][]byte) map[string][]string {
	known := map[string]bool{}
	for p := range files {
		if n, ok := skillName(p); ok {
			known[n] = true
		}
	}
	graph := map[string][]string{}
	for p, body := range files {
		n, ok := skillName(p)
		if !ok {
			continue
		}
		for _, m := range wikilinkRe.FindAllStringSubmatch(string(body), -1) {
			if target := m[1]; known[target] && target != n {
				graph[n] = append(graph[n], target)
			}
		}
	}
	return graph
}

func skillName(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, ".claude/skills/")
	if !ok {
		return "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return "", false
	}
	return rest[:i], true
}

// closeOver returns the transitive closure of seeds over the skill link graph,
// sorted. Closure — rather than the two directly-named skills — is what
// guarantees a seated skill never contains a [[link]] to an unseated one.
func closeOver(seeds []string, graph map[string][]string) []string {
	seen := map[string]bool{}
	stack := append([]string{}, seeds...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, graph[n]...)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// parseCharters maps each agent stem to its `tools:` frontmatter list.
func parseCharters(files map[string][]byte) map[string][]string {
	out := map[string][]string{}
	for p, body := range files {
		rest, ok := strings.CutPrefix(p, ".claude/agents/")
		if !ok || !strings.HasSuffix(rest, ".md") {
			continue
		}
		out[strings.TrimSuffix(rest, ".md")] = parseToolsBlock(string(body))
	}
	return out
}

// parseToolsBlock extracts the YAML `tools:` sequence from a charter's
// frontmatter. It stops at the frontmatter terminator or the next key, so
// prose in the body can never bleed into the list.
func parseToolsBlock(body string) []string {
	fm, _, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---")
	if !ok {
		return nil
	}
	tools := []string{}
	inBlock := false
	for _, l := range strings.Split(fm, "\n") {
		if strings.HasPrefix(l, "tools:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		m := toolLineRe.FindStringSubmatch(l)
		if m == nil {
			break // next key, or a non-item line: the sequence ended
		}
		tools = append(tools, m[1])
	}
	return tools
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// rendering
// ---------------------------------------------------------------------------

func render(manifests map[string]methodassets.StepManifest) ([]byte, error) {
	slugs := make([]string, 0, len(manifests))
	for s := range manifests {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	var b bytes.Buffer
	b.WriteString("// Code generated by cmd/gen-stepmanifest; DO NOT EDIT.\n\n")
	b.WriteString("package methodassets\n\n")
	b.WriteString("// stepManifests is the per-step prompt surface, one entry per dispatchable\n")
	b.WriteString("// command slug. See stepmanifest.go for what each field means and\n")
	b.WriteString("// cmd/gen-stepmanifest for how each is derived.\n")
	b.WriteString("var stepManifests = map[string]StepManifest{\n")
	for _, s := range slugs {
		m := manifests[s]
		fmt.Fprintf(&b, "\t%q: {\n", s)
		fmt.Fprintf(&b, "\t\tCommand: %q,\n", m.Command)
		fmt.Fprintf(&b, "\t\tAgent:   %q,\n", m.Agent)
		fmt.Fprintf(&b, "\t\tMode:    %q,\n", m.Mode)
		fmt.Fprintf(&b, "\t\tSkills:  []string{%s},\n", quoteList(m.Skills))
		fmt.Fprintf(&b, "\t\tTools:   []string{%s},\n", quoteList(m.Tools))
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func quoteList(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%q", x)
	}
	return "\n\t\t\t" + strings.Join(parts, ",\n\t\t\t") + ",\n\t\t"
}
