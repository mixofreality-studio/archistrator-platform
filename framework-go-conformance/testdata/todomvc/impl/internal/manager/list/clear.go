package list

import (
	"go.temporal.io/sdk/workflow"
)

func (m *listManager) clearWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "clearCompleted"); err != nil {
		return ChangeOutcome{}, err
	}
	after, err := invokers().TodoListAccessPurgeCompleted(ctx, in.Owner)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.finish("cleared", nil, after, true)
}
