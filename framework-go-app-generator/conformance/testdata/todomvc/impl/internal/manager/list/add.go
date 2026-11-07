package list

import (
	"crypto/sha256"
	"encoding/hex"

	"go.temporal.io/sdk/workflow"
)

func (m *listManager) addWorkflow(ctx workflow.Context, in opInput) (ChangeOutcome, error) {
	if err := m.admit(in, "add"); err != nil {
		return ChangeOutcome{}, err
	}
	ruling, err := m.ruling.JudgeTitle(m.ec(in), in.Wording)
	if err != nil {
		return ChangeOutcome{}, engineFailure(err)
	}
	if !ruling.Accepted {
		return flow{ctx: ctx, m: m, in: in}.recallThenFinish("notAdded", nil, false)
	}
	id, err := flow{ctx: ctx, m: m, in: in}.mintID()
	if err != nil {
		return ChangeOutcome{}, err
	}
	todo, err := invokers().TodoListAccessRecordTodo(ctx, in.Owner, id, ruling.Title)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return flow{ctx: ctx, m: m, in: in}.recallThenFinish("added", toTodo(todo), true)
}

// mintID derives the todo id from owner + requestId; an empty requestId mints a fresh id.
func (f flow) mintID() (string, error) {
	if f.in.RequestID != "" {
		sum := sha256.Sum256([]byte(f.in.Owner + "\x00" + f.in.RequestID))
		return hex.EncodeToString(sum[:16]), nil
	}
	var id string
	if err := workflow.SideEffect(f.ctx, func(workflow.Context) any { return randomHex() }).Get(&id); err != nil {
		return "", listFailure(kindUnavailable, "could not mint a todo id", "")
	}
	return id, nil
}
