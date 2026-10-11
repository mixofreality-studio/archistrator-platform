package methodassets

import (
	"slices"
	"strings"
	"testing"
)

func TestMethodSpecForServiceConstruction(t *testing.T) {
	s, err := MethodSpecFor("service", "construction", []string{"agentic-job-access", "C-agentic-job-access"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Command != "service-construction" || s.WorkerClass != "junior-developer" || s.Model != "sonnet" {
		t.Fatalf("identity/model = %+v", s)
	}
	if s.ExitCriterion == "" || s.ArtifactKind != "Construction" || s.Mode != "construct" {
		t.Fatalf("phase facts = %+v", s)
	}
	if strings.Contains(s.Prompt, "$ARGUMENTS") || !strings.Contains(s.Prompt, "agentic-job-access C-agentic-job-access") {
		t.Fatalf("prompt not rendered:\n%s", s.Prompt)
	}
	if strings.HasPrefix(s.Charter, "---") || s.Charter == "" {
		t.Fatalf("charter not stripped of frontmatter")
	}
	if !slices.Contains(s.MCPTools, "run_checks") || !slices.Contains(s.BuiltinTools, "Bash") {
		t.Fatalf("tools = %v %v", s.MCPTools, s.BuiltinTools)
	}
}

func TestMethodSpecForReviewWithoutCommand(t *testing.T) {
	s, err := MethodSpecFor("service", "codeReview", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != LifecycleTaskReview || s.Command != "" || s.Prompt != "" || len(s.MCPTools) != 0 {
		t.Fatalf("review spec = %+v", s)
	}
	if s.ArtifactKind != "Construction" { // codeReview reviews construction
		t.Fatalf("artifactKind = %q", s.ArtifactKind)
	}
}

func TestMethodSpecForEveryLifecycleTask(t *testing.T) {
	for _, l := range Lifecycles() {
		for _, task := range l.Tasks {
			s, err := MethodSpecFor(l.Type, task.ID, []string{"c", "a"})
			if err != nil {
				t.Errorf("%s/%s: %v", l.Type, task.ID, err)
				continue
			}
			if task.Command != "" && (s.Model == "" || s.Prompt == "") {
				t.Errorf("%s/%s: dispatchable task has empty model or prompt", l.Type, task.ID)
			}
		}
	}
}

func TestMethodSpecForUnknown(t *testing.T) {
	if _, err := MethodSpecFor("service", "nope", nil); err == nil {
		t.Fatal("unknown task resolved")
	}
	if _, err := MethodSpecFor("nope", "construction", nil); err == nil {
		t.Fatal("unknown lifecycle resolved")
	}
}

func TestRenderCommandDesignTakesNoArguments(t *testing.T) {
	p, err := RenderCommand("mission-draft", nil)
	if err != nil || p == "" || strings.Contains(p, "$ARGUMENTS") {
		t.Fatalf("RenderCommand(mission-draft) = %q, %v", p, err)
	}
}
