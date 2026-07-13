package methodassets

import (
	"strings"
	"testing"
)

func TestClaudeFilesKeysAreRepoRelative(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("ClaudeFiles returned no files")
	}
	for path, body := range files {
		if !strings.HasPrefix(path, ".claude/") {
			t.Errorf("key %q must start with .claude/", path)
		}
		if len(body) == 0 {
			t.Errorf("file %q is empty", path)
		}
	}
}

func TestClaudeFilesInventory(t *testing.T) {
	files, err := ClaudeFiles()
	if err != nil {
		t.Fatal(err)
	}
	agents, commands, skills := 0, 0, map[string]bool{}
	for p := range files {
		switch {
		case strings.HasPrefix(p, ".claude/agents/"):
			agents++
		case strings.HasPrefix(p, ".claude/commands/"):
			commands++
		case strings.HasPrefix(p, ".claude/skills/"):
			parts := strings.Split(p, "/")
			skills[parts[2]] = true
		}
	}
	if agents != 10 {
		t.Errorf("agents = %d, want 10", agents)
	}
	if commands != 51 { // grows to 56 in Task 6; update there
		t.Errorf("commands = %d, want 51", commands)
	}
	if len(skills) != 27 {
		t.Errorf("skill dirs = %d, want 27", len(skills))
	}
	if skills["grillme"] {
		t.Error("grillme must NOT be lifted (archistrator-local)")
	}
	for p := range files {
		if strings.Contains(p, "structurizr") || strings.HasPrefix(p, ".claude/hooks/") {
			t.Errorf("cruft lifted: %s", p)
		}
	}
}

func TestDesignDraftCommandsExist(t *testing.T) {
	files, _ := ClaudeFiles()
	for _, name := range []string{
		"mission-draft", "glossary-draft", "scrubbed-requirements-draft",
		"volatilities-draft", "core-use-cases-draft", "system-draft",
		"operational-concepts-draft", "standard-check-draft",
		"planning-assumptions-draft", "activity-list-draft", "network-draft",
		"normal-solution-draft", "subcritical-solution-draft",
		"decompressed-solution-draft", "compressed-solution-draft",
		"risk-model-draft",
	} {
		if _, ok := files[".claude/commands/"+name+".md"]; !ok {
			t.Errorf("missing draft command %s", name)
		}
	}
}
