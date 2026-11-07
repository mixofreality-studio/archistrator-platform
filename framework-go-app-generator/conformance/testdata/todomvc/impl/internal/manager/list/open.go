package list

import (
	"go.temporal.io/sdk/workflow"

	"example.com/conformance/todomvc/internal/engine/listruling"
)

func (m *listManager) openWorkflow(ctx workflow.Context, in opInput) (ListView, error) {
	if err := m.admit(in, "read"); err != nil {
		return ListView{}, err
	}
	l, err := invokers().TodoListAccessRecallList(ctx, in.Owner)
	if err != nil {
		return ListView{}, activityFailure(err)
	}
	all := ruled(l)
	visible, err := m.ruling.SelectView(m.ec(in), all, listruling.View(in.View))
	if err != nil {
		return ListView{}, engineFailure(err)
	}
	left, err := m.ruling.TallyItemsLeft(m.ec(in), all)
	if err != nil {
		return ListView{}, engineFailure(err)
	}
	todos := make([]Todo, 0, len(visible.Todos))
	for _, t := range visible.Todos {
		todos = append(todos, Todo{Id: t.Id, Title: t.Title, Completed: t.Completed})
	}
	return ListView{
		Owner:      in.Owner,
		View:       in.View,
		Todos:      todos,
		ItemsLeft:  left.Count,
		TotalCount: int64(len(all)),
	}, nil
}
