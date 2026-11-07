package list

import (
	"go.temporal.io/sdk/workflow"
)

func (m *listManager) markWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "mark"); err != nil {
		return ChangeOutcome{}, err
	}
	if in.TodoID == "" {
		return ChangeOutcome{}, listFailure(kindInvalidRequest, "todoId is empty", "")
	}
	todo, err := invokers().TodoListAccessSetCompletion(ctx, in.Owner, in.TodoID, in.Completed)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.recallThenFinish("marked", toTodo(todo), true)
}
