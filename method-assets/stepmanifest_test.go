package methodassets

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// wikilinkPattern matches a [[skill-name]] cross-reference in a skill body.
var wikilinkPattern = regexp.MustCompile(`\[\[([a-z0-9-]+)\]\]`)

// TestStepManifestRegenerationIsNoOp is the drift gate: the committed
// stepmanifest.gen.go must be exactly what cmd/gen-stepmanifest emits from the
// current assets. Edit a command's "Agent + skills" line, a skill's links, or a
// charter's tools, and this fails until the manifest is regenerated.
func TestStepManifestRegenerationIsNoOp(t *testing.T) {
	before, err := os.ReadFile("stepmanifest.gen.go")
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}
	dir := t.TempDir()
	tmp := filepath.Join(dir, "stepmanifest.gen.go")
	if cerr := os.WriteFile(tmp, before, 0o600); cerr != nil {
		t.Fatalf("stage: %v", cerr)
	}

	cmd := exec.Command("go", "run", "./cmd/gen-stepmanifest")
	cmd.Dir = "."
	if out, rerr := cmd.CombinedOutput(); rerr != nil {
		t.Fatalf("go run ./cmd/gen-stepmanifest: %v\n%s", rerr, out)
	}
	after, err := os.ReadFile("stepmanifest.gen.go")
	if err != nil {
		t.Fatalf("read regenerated manifest: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("stepmanifest.gen.go is stale — run: go run ./cmd/gen-stepmanifest")
	}
}

// TestEveryConstructStepHoldsOperatorNotes pins the prompt half of operator-note
// delivery (archistrator B1, amendment §C.1 item 3): every construct step is granted
// get_operator_notes, no other mode is, and every construct command tells the agent to
// call it first. A construct step without the grant would have the tool filtered out of
// its MCP surface while its command tells it to call the tool.
func TestEveryConstructStepHoldsOperatorNotes(t *testing.T) {
	const tool = "mcp__aiarch-state__get_operator_notes"
	const preamble = "First call `get_operator_notes`; if it returns notes, act on them before anything else."
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	construct := 0
	for slug, m := range Manifests() {
		has := false
		for _, name := range m.Tools {
			if name == tool {
				has = true
			}
		}
		body := string(files[".claude/commands/"+slug+".md"])
		switch m.Mode {
		case "construct":
			construct++
			if !has {
				t.Errorf("%s: a construct step must be granted get_operator_notes", slug)
			}
			if !strings.Contains(body, preamble) {
				t.Errorf("%s: a construct command must tell the agent to call get_operator_notes first", slug)
			}
		default:
			if has {
				t.Errorf("%s (mode %s): only construct steps are granted get_operator_notes", slug, m.Mode)
			}
			if strings.Contains(body, "get_operator_notes") {
				t.Errorf("%s (mode %s): only construct commands name get_operator_notes", slug, m.Mode)
			}
		}
	}
	if construct < 20 {
		t.Fatalf("found %d construct steps; the manifest lost the construction commands", construct)
	}
}

// TestEveryDispatchableCommandHasAManifest asserts the manifest covers exactly
// the dispatchable command set: every command file either has an entry or is a
// known non-dispatchable orchestration command, and no entry names a command
// that does not exist.
func TestEveryDispatchableCommandHasAManifest(t *testing.T) {
	// Mirrors gen-stepmanifest's orchestrationCommands.
	orchestration := map[string]bool{
		"system-design": true, "project-design": true, "add-use-case": true,
		"implement-project": true, "sdp-review": true,
	}
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}

	onDisk := map[string]bool{}
	for p := range files {
		rest, ok := strings.CutPrefix(p, ".claude/commands/")
		if !ok || !strings.HasSuffix(rest, ".md") {
			continue
		}
		onDisk[strings.TrimSuffix(rest, ".md")] = true
	}

	for slug := range onDisk {
		_, has := ManifestFor(slug)
		if !has && !orchestration[slug] {
			t.Errorf("command %q has no step manifest (add it, or list it as orchestration)", slug)
		}
		if has && orchestration[slug] {
			t.Errorf("orchestration command %q must not have a step manifest", slug)
		}
	}
	for slug := range Manifests() {
		if !onDisk[slug] {
			t.Errorf("manifest names command %q, which has no command file", slug)
		}
	}
}

// TestManifestAssetsExist asserts every agent and skill a manifest names is
// really in the asset set — a manifest can never seat a file that isn't there.
func TestManifestAssetsExist(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}
	skills := map[string]bool{}
	for p := range files {
		if n, ok := strings.CutPrefix(p, ".claude/skills/"); ok {
			if i := strings.IndexByte(n, '/'); i > 0 {
				skills[n[:i]] = true
			}
		}
	}
	for slug, m := range Manifests() {
		if m.Agent != "" {
			if _, ok := files[".claude/agents/"+m.Agent+".md"]; !ok {
				t.Errorf("%s: agent charter %q does not exist", slug, m.Agent)
			}
		}
		for _, s := range m.Skills {
			if !skills[s] {
				t.Errorf("%s: skill %q does not exist", slug, s)
			}
		}
		if len(m.Tools) == 0 {
			t.Errorf("%s: manifest grants no tools", slug)
		}
	}
}

// TestSeatedSkillsAreClosed asserts each manifest's skill set is closed under
// skill-to-skill [[links]]: a seated skill must never point at an unseated
// one, which would send the agent chasing a skill that isn't on disk.
func TestSeatedSkillsAreClosed(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}
	known := map[string]bool{}
	for p := range files {
		if n, ok := strings.CutPrefix(p, ".claude/skills/"); ok {
			if i := strings.IndexByte(n, '/'); i > 0 {
				known[n[:i]] = true
			}
		}
	}
	for slug, m := range Manifests() {
		seated := map[string]bool{}
		for _, s := range m.Skills {
			seated[s] = true
		}
		for _, s := range m.Skills {
			body, ok := files[".claude/skills/"+s+"/SKILL.md"]
			if !ok {
				continue
			}
			for _, mm := range wikilinkPattern.FindAllStringSubmatch(string(body), -1) {
				target := mm[1]
				if known[target] && !seated[target] {
					t.Errorf("%s: seated skill %q links to unseated skill %q", slug, s, target)
				}
			}
		}
	}
}

// TestManifestCoversObservedUsage is the anti-starvation gate. testdata/
// observed-tool-usage.json records every tool each step was actually seen to
// call across the archived archistrator-bench todomvc runs. A manifest that
// omits a tool its step demonstrably uses would break that step at runtime —
// expensively, since construction episodes are slow. Tightening the manifest
// must never drop below observed reality.
func TestManifestCoversObservedUsage(t *testing.T) {
	var fixture struct {
		Usage map[string][]string `json:"usage"`
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "observed-tool-usage.json"))
	if err != nil {
		t.Fatalf("read observed-usage fixture: %v", err)
	}
	if uerr := json.Unmarshal(raw, &fixture); uerr != nil {
		t.Fatalf("decode observed-usage fixture: %v", uerr)
	}
	if len(fixture.Usage) == 0 {
		t.Fatal("observed-usage fixture is empty")
	}

	for slug, observed := range fixture.Usage {
		m, ok := ManifestFor(slug)
		if !ok {
			t.Errorf("observed usage for %q, which has no manifest", slug)
			continue
		}
		granted := map[string]bool{}
		for _, tool := range m.Tools {
			granted[tool] = true
		}
		var missing []string
		for _, tool := range observed {
			if !granted[tool] {
				missing = append(missing, tool)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s: manifest omits tools this step was OBSERVED to call: %v", slug, missing)
		}
	}
}

// TestClaudeFilesForIsScoped asserts the scoped asset set really is scoped:
// exactly one command, at most one charter, only the manifest's skills — and
// that it is strictly smaller than the full tree.
func TestClaudeFilesForIsScoped(t *testing.T) {
	all, err := ClaudeFiles()
	if err != nil {
		t.Fatalf("ClaudeFiles: %v", err)
	}
	for _, slug := range []string{"mission-draft", "service-construction"} {
		got, ferr := ClaudeFilesFor(slug)
		if ferr != nil {
			t.Fatalf("ClaudeFilesFor(%s): %v", slug, ferr)
		}
		if len(got) >= len(all) {
			t.Errorf("%s: scoped set (%d files) is not smaller than the full tree (%d)", slug, len(got), len(all))
		}
		m, _ := ManifestFor(slug)
		var cmds, agents int
		for p := range got {
			switch {
			case strings.HasPrefix(p, ".claude/commands/"):
				cmds++
				if p != ".claude/commands/"+slug+".md" {
					t.Errorf("%s: scoped set leaked command %s", slug, p)
				}
			case strings.HasPrefix(p, ".claude/agents/"):
				agents++
				if p != ".claude/agents/"+m.Agent+".md" {
					t.Errorf("%s: scoped set leaked charter %s", slug, p)
				}
			case strings.HasPrefix(p, ".claude/skills/"):
				name := skillNameOf(p)
				if !contains(m.Skills, name) {
					t.Errorf("%s: scoped set leaked skill %s", slug, name)
				}
			}
		}
		if cmds != 1 {
			t.Errorf("%s: expected exactly 1 command file, got %d", slug, cmds)
		}
		if agents != 1 {
			t.Errorf("%s: expected exactly 1 agent charter, got %d", slug, agents)
		}
	}

	if _, err := ClaudeFilesFor("system-design"); err == nil {
		t.Error("ClaudeFilesFor should reject a non-dispatchable orchestration command")
	}
}

// TestMaterializeStepNarrows asserts re-seating a worktree for a different
// step SWAPS the surface rather than accumulating it: after a full Materialize
// then a MaterializeStep, only the step's assets remain.
func TestMaterializeStepNarrows(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	full, _ := filepath.Glob(filepath.Join(dir, ".claude", "commands", "*.md"))
	if len(full) < 50 {
		t.Fatalf("expected a full command set seated, got %d", len(full))
	}
	if err := MaterializeStep(dir, "mission-draft"); err != nil {
		t.Fatalf("MaterializeStep: %v", err)
	}
	scoped, _ := filepath.Glob(filepath.Join(dir, ".claude", "commands", "*.md"))
	if len(scoped) != 1 {
		t.Errorf("after scoped seat: expected 1 command on disk, got %d", len(scoped))
	}
	charters, _ := filepath.Glob(filepath.Join(dir, ".claude", "agents", "*.md"))
	if len(charters) != 1 {
		t.Errorf("after scoped seat: expected 1 charter on disk, got %d", len(charters))
	}
	m, _ := ManifestFor("mission-draft")
	skills, _ := filepath.Glob(filepath.Join(dir, ".claude", "skills", "*"))
	if len(skills) != len(m.Skills) {
		t.Errorf("after scoped seat: expected %d skills on disk, got %d", len(m.Skills), len(skills))
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
