package list

import (
	"go.temporal.io/sdk/workflow"
)

func (m *listManager) discardWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "discard"); err != nil {
		return ChangeOutcome{}, err
	}
	if in.TodoID == "" {
		return ChangeOutcome{}, listFailure(kindInvalidRequest, "todoId is empty", "")
	}
	if _, err := invokers().TodoListAccessRemoveTodo(ctx, in.Owner, in.TodoID); err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.recallThenFinish("removed", nil, true)
}
