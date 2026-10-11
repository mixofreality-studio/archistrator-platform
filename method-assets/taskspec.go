package methodassets

import (
	"fmt"
	"strings"
)

// MethodSpec is the method half of an agent task spec: everything the embedded
// lifecycle, step manifest, command, charter and skill assets decide about one
// lifecycle task, with no project data. archistrator's projectstate.TaskSpecFor
// adds the project half (edit scope, checks, cost, hash).
type MethodSpec struct {
	LifecycleKey  string
	TaskID        string
	Kind          string // LifecycleTaskDispatch | LifecycleTaskReview
	Title         string
	Phase         string
	Command       string // "" for a review task with no command
	Agent         string // charter stem; == WorkerClass
	WorkerClass   string
	ArtifactKind  string // task's, else the reviewed task's
	ExitCriterion string // the phase's
	Mode          string // manifest mode
	Model         string // charter model alias
	Prompt        string // command body, $ARGUMENTS substituted
	Charter       string // charter body (frontmatter stripped)
	Skills        []string
	MCPTools      []string // sorted, prefix-stripped
	BuiltinTools  []string // sorted
}

// MethodSpecFor resolves one lifecycle task. args are the command's
// $ARGUMENTS tokens (<component_id> <activity_id> for construction commands,
// none for design commands). A review task with no command resolves to its
// identity only.
func MethodSpecFor(lifecycleKey, taskID string, args []string) (MethodSpec, error) {
	l, ok := LifecycleFor(lifecycleKey)
	if !ok {
		return MethodSpec{}, fmt.Errorf("no lifecycle %q", lifecycleKey)
	}
	var task *LifecycleTask
	for i := range l.Tasks {
		if l.Tasks[i].ID == taskID {
			task = &l.Tasks[i]
		}
	}
	if task == nil {
		return MethodSpec{}, fmt.Errorf("lifecycle %q has no task %q", lifecycleKey, taskID)
	}
	s := MethodSpec{
		LifecycleKey: lifecycleKey, TaskID: taskID, Kind: task.Kind, Title: task.Title,
		Phase: task.Phase, Command: task.Command, Agent: task.WorkerClass, WorkerClass: task.WorkerClass,
		ArtifactKind: artifactKindOf(l, *task),
	}
	for _, p := range l.Phases {
		if p.ID == task.Phase {
			s.ExitCriterion = p.ExitCriterion
		}
	}
	if task.Command == "" {
		return s, nil
	}
	m, ok := ManifestFor(task.Command)
	if !ok {
		return MethodSpec{}, fmt.Errorf("task %s/%s: no manifest for command %q", lifecycleKey, taskID, task.Command)
	}
	files, err := ClaudeFiles()
	if err != nil {
		return MethodSpec{}, err
	}
	prompt, err := RenderCommand(task.Command, args)
	if err != nil {
		return MethodSpec{}, err
	}
	s.Mode, s.Model, s.Prompt = m.Mode, m.Model, prompt
	s.Charter = stripFrontmatter(string(files[".claude/agents/"+m.Agent+".md"]))
	s.Skills = append([]string(nil), m.Skills...)
	s.MCPTools, s.BuiltinTools = m.MCPTools(), m.BuiltinTools()
	return s, nil
}

// RenderCommand is what Claude Code expands "/<command> <args>" into: the
// command body with $ARGUMENTS replaced by the space-joined args.
func RenderCommand(command string, args []string) (string, error) {
	files, err := ClaudeFiles()
	if err != nil {
		return "", err
	}
	body, ok := files[".claude/commands/"+command+".md"]
	if !ok {
		return "", fmt.Errorf("no command %q", command)
	}
	return strings.ReplaceAll(string(body), "$ARGUMENTS", strings.Join(args, " ")), nil
}

func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	_, rest, ok := strings.Cut(strings.TrimPrefix(body, "---\n"), "\n---")
	if !ok {
		return body
	}
	return strings.TrimLeft(rest, "\n")
}

// artifactKindOf mirrors webApp activityViewToGraph.artifactKindOf: the task's
// own kind, else the reviewed task's.
func artifactKindOf(l Lifecycle, t LifecycleTask) string {
	if t.ArtifactKind != "" {
		return t.ArtifactKind
	}
	for _, o := range l.Tasks {
		if o.ID == t.Reviews && o.ArtifactKind != "" {
			return o.ArtifactKind
		}
	}
	return ""
}
