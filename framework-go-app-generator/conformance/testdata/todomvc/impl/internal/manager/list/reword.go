package list

import (
	"go.temporal.io/sdk/workflow"
)

func (m *listManager) rewordWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "reword"); err != nil {
		return ChangeOutcome{}, err
	}
	if in.TodoID == "" {
		return ChangeOutcome{}, listFailure(kindInvalidRequest, "todoId is empty", "")
	}
	ruling, err := m.ruling.JudgeTitle(m.ec(in), in.Wording)
	if err != nil {
		return ChangeOutcome{}, engineFailure(err)
	}
	if ruling.Accepted {
		todo, err := invokers().TodoListAccessRetitleTodo(ctx, in.Owner, in.TodoID, ruling.Title)
		if err != nil {
			return ChangeOutcome{}, activityFailure(err)
		}
		return flow{ctx: ctx, m: m, in: in}.recallThenFinish("retitled", toTodo(todo), true)
	}
	if _, err := invokers().TodoListAccessRemoveTodo(ctx, in.Owner, in.TodoID); err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.recallThenFinish("removed", nil, true)
}
