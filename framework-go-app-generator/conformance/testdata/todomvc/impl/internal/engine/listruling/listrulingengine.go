// Package listruling implements ListRulingEngine: the pure, deterministic
// TodoMVC ruling shared by every host.
package listruling

import (
	"strings"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

type listRulingEngine struct{}

func newListRulingEngine() ListRulingEngine {
	return &listRulingEngine{}
}

// JudgeTitle trims surrounding Unicode whitespace; a blank result is not accepted.
func (e *listRulingEngine) JudgeTitle(_ fweng.Context, wording string) (TitleRuling, error) {
	t := strings.TrimSpace(wording)
	if t == "" {
		return TitleRuling{Accepted: false, Title: ""}, nil
	}
	return TitleRuling{Accepted: true, Title: t}, nil
}

// JudgeToggleTarget rules markCompleted=false only when every todo is completed
// (vacuously true for the empty list).
func (e *listRulingEngine) JudgeToggleTarget(_ fweng.Context, todos []RuledTodo) (ToggleTarget, error) {
	for _, t := range todos {
		if !t.Completed {
			return ToggleTarget{MarkCompleted: true}, nil
		}
	}
	return ToggleTarget{MarkCompleted: false}, nil
}

// SelectView returns the todos visible in the view, preserving list order.
func (e *listRulingEngine) SelectView(_ fweng.Context, todos []RuledTodo, view View) (VisibleTodos, error) {
	var keep func(RuledTodo) bool
	switch view {
	case "all":
		keep = func(RuledTodo) bool { return true }
	case "active":
		keep = func(t RuledTodo) bool { return !t.Completed }
	case "completed":
		keep = func(t RuledTodo) bool { return t.Completed }
	default:
		return VisibleTodos{}, fweng.New(fweng.ContractMisuse, "UnknownView: "+string(view))
	}
	out := make([]RuledTodo, 0, len(todos))
	for _, t := range todos {
		if keep(t) {
			out = append(out, t)
		}
	}
	return VisibleTodos{Todos: out}, nil
}

// TallyItemsLeft counts active todos across the whole list.
func (e *listRulingEngine) TallyItemsLeft(_ fweng.Context, todos []RuledTodo) (ItemsLeft, error) {
	var n int64
	for _, t := range todos {
		if !t.Completed {
			n++
		}
	}
	return ItemsLeft{Count: n}, nil
}
