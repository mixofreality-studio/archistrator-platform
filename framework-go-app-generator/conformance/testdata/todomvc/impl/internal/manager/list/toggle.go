package list

import (
	"go.temporal.io/sdk/workflow"
)

func (m *listManager) toggleWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "toggleAll"); err != nil {
		return ChangeOutcome{}, err
	}
	l, err := invokers().TodoListAccessRecallList(ctx, in.Owner)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	target, err := m.ruling.JudgeToggleTarget(m.ec(in), ruled(l))
	if err != nil {
		return ChangeOutcome{}, engineFailure(err)
	}
	after, err := invokers().TodoListAccessSetAllCompletion(ctx, in.Owner, target.MarkCompleted)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.finish("toggled", nil, after, true)
}
