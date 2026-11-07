// Package accessgranting decides whether a requester may act on a member's todo list.
package accessgranting

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

const (
	kindMember RequesterKind = "member"
	kindAgent  RequesterKind = "agent"

	reasonNone               RefusalReason = ""
	reasonNotOwner           RefusalReason = "notOwner"
	reasonOutsideDelegation  RefusalReason = "outsideDelegation"
	reasonIncompleteQuestion RefusalReason = "incompleteQuestion"
)

type accessGrantingEngine struct{}

func newAccessGrantingEngine() AccessGrantingEngine {
	return &accessGrantingEngine{}
}

func validIntent(i Intent) bool {
	switch i {
	case "read", "add", "reword", "discard", "mark", "toggleAll", "clearCompleted":
		return true
	}
	return false
}

func refuse(r RefusalReason) (AccessDecision, error) {
	return AccessDecision{Permitted: false, Reason: r}, nil
}

// DecideAccess is pure and fails closed; a refusal is a decision, never an error.
func (e *accessGrantingEngine) DecideAccess(_ fweng.Context, q AccessQuestion) (AccessDecision, error) {
	if q.RequesterSubject == "" || q.Owner == "" || !validIntent(q.Intent) {
		return refuse(reasonIncompleteQuestion)
	}
	switch q.RequesterKind {
	case kindMember:
		if q.RequesterSubject == q.Owner {
			return AccessDecision{Permitted: true, Reason: reasonNone}, nil
		}
		return refuse(reasonNotOwner)
	case kindAgent:
		for _, d := range q.Delegations {
			if d.Grantor != q.Owner {
				continue
			}
			for _, in := range d.Intents {
				if in == q.Intent {
					return AccessDecision{Permitted: true, Reason: reasonNone}, nil
				}
			}
		}
		return refuse(reasonOutsideDelegation)
	}
	return refuse(reasonIncompleteQuestion)
}
