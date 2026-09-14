package methodassets

// stepmanifest.go — PER-STEP PROMPT-SURFACE SCOPING.
//
// PROBLEM. Materialize seats the ENTIRE .claude tree (59 commands, 28 skills,
// 10 agents) for every dispatch, and cmd/aiarch-state-mcp registered every
// read-only generated tool in every job mode. A Mission draft therefore ran
// with 45 MCP tools in its context — including billingComputeNet,
// autoscalerProposeDesiredState, revenueLedgerReadRange and
// operatedRuntimeGetSloStatus — none of which that step can legally call.
// Measured on benchmark run-20260815T190639Z: ~44.6k tokens of prompt prefix
// re-read on every one of 491 turns, and a tool surface large enough that
// Claude Code deferred it, spending 31 turns re-discovering its own tools.
//
// SOLUTION. One manifest per dispatchable command slug, declaring exactly the
// agent charter, skills, and tools that step may use. Three consumers read it:
//
//  1. MaterializeStep — seats only the named command + its agent + its skill
//     closure (the rest is pruned by the existing manifest prune rail).
//  2. cmd/aiarch-state-mcp buildServer — registers only Tools' mcp__ entries.
//  3. the executor's claudeArgv — derives --disallowedTools from Tools.
//
// DERIVED, NOT HAND-MAINTAINED. The manifest is GENERATED (see
// cmd/gen-stepmanifest) from sources that already exist and are already
// tested:
//
//   - the agent: the command's "**Agent + skills.**" line, the same line
//     commandwrites_test.go's agentsForCommand already parses;
//   - the skills: the [[wikilinks]] on that line, transitively closed over
//     skill-to-skill links so a seated skill never points at an unseated one;
//   - the tools: the agent charter's `tools:` frontmatter (already asserted by
//     frontmatter_test.go) INTERSECTED with what the step's job mode can
//     legally call, MINUS the project-design estimation surface for steps that
//     are not project-design steps.
//
// stepmanifest_test.go asserts regeneration is a no-op, that every dispatchable
// command has an entry, that every named asset exists, and that each entry is a
// superset of what that step was OBSERVED to call across the archived benchmark
// runs — so tightening the manifest can never silently starve a step of a tool
// it actually uses.

import (
	"fmt"
	"sort"
	"strings"
)

// StepManifest is the exact prompt surface one dispatchable step may use.
type StepManifest struct {
	// Command is the slash-command slug the dispatcher passes as the prompt
	// (e.g. "mission-draft", "service-construction"). It is the manifest key.
	Command string
	// Agent is the charter stem the command tells the session to adopt
	// (".claude/agents/<Agent>.md"). Empty only for steps with no charter.
	Agent string
	// Skills is the transitively-closed set of skill names this step seats.
	Skills []string
	// Tools is the full allowlist: built-in tool names (Read, Bash, ...) and
	// fully-qualified MCP tool names (mcp__aiarch-state__getDraftSlot).
	Tools []string
	// Mode is the job mode this step runs under: draft, critique, answer, or
	// construct. It is what narrows the charter's tools to the legal set.
	Mode string
}

// MCPTools returns the manifest's MCP tool names with the server prefix
// stripped — the composed-verb / raw-tool names cmd/aiarch-state-mcp
// registers by. Order is stable (sorted).
func (m StepManifest) MCPTools() []string {
	out := []string{}
	for _, t := range m.Tools {
		if rest, ok := strings.CutPrefix(t, mcpToolPrefix); ok {
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out
}

// BuiltinTools returns the manifest's non-MCP tool names — the built-in
// Claude Code tools this step is allowed to use. Order is stable (sorted).
func (m StepManifest) BuiltinTools() []string {
	out := []string{}
	for _, t := range m.Tools {
		if !strings.HasPrefix(t, mcpToolPrefix) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// mcpToolPrefix is the aiarch-state MCP server's tool-name prefix as it
// appears in agent charters and in a Claude Code session's tool list.
const mcpToolPrefix = "mcp__aiarch-state__"

// scopableBuiltinTools is the set of Claude Code BUILT-IN tools a dispatch may
// switch off for a step. It lives here, beside the manifest, because BOTH
// dispatch rails need it and they must agree: the local executor reads it in
// process, and the GitHub Actions rail reads it via `aiarch-state-mcp
// step-tools`. A second copy in either consumer would be free to drift.
//
// It is a CLOSED DENY-CANDIDATE list, not a complement of what is granted, and
// that direction matters: --disallowedTools names tools to REMOVE, so a name
// that disappears from a future CLI version is simply inert, whereas a
// complement would silently begin denying newly-added tools nobody has vetted.
//
// Read/Write/Edit/Glob/Grep/Bash/Skill are deliberately ABSENT — every charter
// grants some of them, and a step that needs one and cannot call it fails hard.
// What is listed is the session-management, scheduling, messaging and
// orchestration surface an autonomous single-session CI agent cannot use: it
// has no user to message, no schedule to keep, and no subagents to dispatch
// (every command instructs the session to adopt the role itself). Their schemas
// are among the largest in the prompt prefix, so removing them is the cheapest
// available win.
var scopableBuiltinTools = []string{
	"Artifact",
	"CronCreate",
	"CronDelete",
	"CronList",
	"DesignSync",
	"EnterWorktree",
	"ExitWorktree",
	"ListAgents",
	"Monitor",
	"NotebookEdit",
	"PushNotification",
	"RemoteTrigger",
	"ReportFindings",
	"ScheduleWakeup",
	"SendMessage",
	"SendUserFile",
	"Task",
	"TaskCreate",
	"TaskGet",
	"TaskList",
	"TaskOutput",
	"TaskStop",
	"TaskUpdate",
	"Workflow",
}

// ScopableBuiltinTools returns the deny-candidate built-in tool names. The
// slice is copied per call; callers may mutate it.
func ScopableBuiltinTools() []string {
	return append([]string{}, scopableBuiltinTools...)
}

// DisallowedBuiltinTools returns the built-in tools to switch off for a step:
// every deny candidate the step's manifest does not grant. A command with no
// manifest denies NOTHING, so a caller that cannot resolve a step keeps the
// unscoped surface rather than losing tools silently.
//
// Only built-ins are considered. The MCP surface is scoped at its source — the
// MCP server never REGISTERS a tool the manifest withholds — which is strictly
// better than denying it at the CLI, because an unregistered tool costs no
// schema in the prompt prefix at all.
func DisallowedBuiltinTools(command string) []string {
	m, ok := ManifestFor(command)
	if !ok {
		return nil
	}
	granted := map[string]bool{}
	for _, t := range m.BuiltinTools() {
		granted[t] = true
	}
	var deny []string
	for _, t := range scopableBuiltinTools {
		if !granted[t] {
			deny = append(deny, t)
		}
	}
	return deny
}

// ManifestFor returns the manifest for a dispatchable command slug. The
// boolean reports whether the slug is dispatchable at all: the human-facing
// orchestration commands (/system-design, /project-design, /add-use-case,
// /implement-project, /sdp-review) are driven by a person or assembled
// server-side, never dispatched to an agent, and have no manifest.
func ManifestFor(command string) (StepManifest, bool) {
	m, ok := stepManifests[command]
	return m, ok
}

// Manifests returns every step manifest keyed by command slug. The map is
// rebuilt per call; callers may mutate it.
func Manifests() map[string]StepManifest {
	out := make(map[string]StepManifest, len(stepManifests))
	for k, v := range stepManifests {
		out[k] = v
	}
	return out
}

// ClaudeFilesFor returns the subset of the .claude tree one step needs: its
// own command file, its agent charter, and its skill closure. Everything else
// — the other 58 commands, the other 9 charters, the unreferenced skills — is
// omitted, and MaterializeStep's prune pass removes any of it left over from
// a previous full seat.
//
// Files outside commands/, agents/ and skills/ (settings, docs the skills
// link to) are ALWAYS included: they are small, shared, and a skill that
// links to a missing doc is exactly the dead reference this scoping is meant
// to avoid creating.
func ClaudeFilesFor(command string) (map[string][]byte, error) {
	m, ok := ManifestFor(command)
	if !ok {
		return nil, fmt.Errorf("method-assets: %q is not a dispatchable command (no step manifest)", command)
	}
	all, err := ClaudeFiles()
	if err != nil {
		return nil, err
	}

	keepSkill := map[string]bool{}
	for _, s := range m.Skills {
		keepSkill[s] = true
	}

	out := map[string][]byte{}
	for p, body := range all {
		if inStepScope(p, m, keepSkill) {
			out[p] = body
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("method-assets: step %q resolved to an empty asset set", command)
	}
	return out, nil
}

// inStepScope reports whether the asset at p belongs to step m's scoped set:
// its own command file, its own charter (a step with no agent has none), a
// skill in its closure, or any shared file outside commands/, agents/ and
// skills/.
func inStepScope(p string, m StepManifest, keepSkill map[string]bool) bool {
	switch {
	case strings.HasPrefix(p, ".claude/commands/"):
		return p == ".claude/commands/"+m.Command+".md"
	case strings.HasPrefix(p, ".claude/agents/"):
		return m.Agent != "" && p == ".claude/agents/"+m.Agent+".md"
	case strings.HasPrefix(p, ".claude/skills/"):
		return keepSkill[skillNameOf(p)]
	default:
		return true
	}
}

// skillNameOf extracts the skill directory name from a seated skill path
// (".claude/skills/<name>/SKILL.md" and any sibling reference file).
func skillNameOf(p string) string {
	rest := strings.TrimPrefix(p, ".claude/skills/")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// MaterializeStep is Materialize scoped to one dispatchable step: it writes
// only that step's asset subset and — via the SAME manifest prune rail
// Materialize uses — removes the files a previous (possibly full) seat owned
// that this step does not need. Seating is therefore idempotent AND
// narrowing: re-seating a worktree for a different step swaps the surface
// rather than accumulating it.
func MaterializeStep(destRepo, command string) error {
	files, err := ClaudeFilesFor(command)
	if err != nil {
		return err
	}
	return materialize(destRepo, files)
}
