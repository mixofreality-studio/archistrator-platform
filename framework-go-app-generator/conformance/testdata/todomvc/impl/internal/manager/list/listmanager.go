// Package list implements ListManager: the list-keeping workflow shared by every door.
// Every operation is one short Temporal workflow: admit -> (recall) -> rule -> keep ->
// tally -> publish.
package list

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"example.com/conformance/todomvc/internal/engine/accessgranting"
	"example.com/conformance/todomvc/internal/engine/listruling"
	"example.com/conformance/todomvc/internal/resourceaccess/todolist"
	"example.com/conformance/todomvc/internal/utility/messagebus"
)

// ListError kinds.
const (
	kindRefused        = "Refused"
	kindTodoNotFound   = "TodoNotFound"
	kindInvalidRequest = "InvalidRequest"
	kindUnavailable    = "Unavailable"
)

// listErrType is the Temporal application-error type carrying a ListError.
const listErrType = "ListError"

// Workflow names (one per operation).
const (
	wfAdd     = "ListManager.AddTodo"
	wfReword  = "ListManager.RewordTodo"
	wfDiscard = "ListManager.DiscardTodo"
	wfMark    = "ListManager.MarkTodo"
	wfToggle  = "ListManager.ToggleAll"
	wfClear   = "ListManager.ClearCompleted"
	wfOpen    = "ListManager.OpenList"
)

// requester is the workflow-serialisable form of the calling principal.
type requester struct {
	Kind        string                      `json:"kind"`
	Subject     string                      `json:"subject"`
	Delegations []accessgranting.Delegation `json:"delegations,omitempty"`
}

// opInput is the input of every workflow.
type opInput struct {
	Requester requester `json:"requester"`
	Owner     string    `json:"owner"`
	TodoID    string    `json:"todoId,omitempty"`
	Wording   string    `json:"wording,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
	Completed bool      `json:"completed,omitempty"`
	View      View      `json:"view,omitempty"`
}

type listManager struct {
	client         client.Client
	messageBus     messagebus.MessageBus
	access         accessgranting.AccessGrantingEngine
	ruling         listruling.ListRulingEngine
	todoListAccess todolist.TodoListAccess
}

func newListManager(c client.Client, bus messagebus.MessageBus, access accessgranting.AccessGrantingEngine, ruling listruling.ListRulingEngine, store todolist.TodoListAccess) ListManager {
	return &listManager{client: c, messageBus: bus, access: access, ruling: ruling, todoListAccess: store}
}

// WorkerManifest lists the workflows, activity options and activity dependencies.
func (m *listManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: wfAdd, Fn: m.addWorkflow},
			{Name: wfReword, Fn: m.rewordWorkflow},
			{Name: wfDiscard, Fn: m.discardWorkflow},
			{Name: wfMark, Fn: m.markWorkflow},
			{Name: wfToggle, Fn: m.toggleWorkflow},
			{Name: wfClear, Fn: m.clearWorkflow},
			{Name: wfOpen, Fn: m.openWorkflow},
		},
		ActivityOptions: activityOptions,
		Activities: genActivities{
			MessageBus:     m.messageBus,
			TodoListAccess: m.todoListAccess,
		},
	}
}

// activityOptions retries only transient infrastructure failure.
func activityOptions(string) (workflow.ActivityOptions, bool) {
	var terminal []fwra.Kind
	for _, k := range fwra.Kinds() {
		if !k.DefaultRetryable() {
			terminal = append(terminal, k)
		}
	}
	return fwm.ActivityPreset{
		Timeout:         15 * time.Second,
		MaxAttempts:     5,
		TerminalRA:      terminal,
		TerminalManager: []fwm.Kind{fwm.ContractMisuse, fwm.NotFound, fwm.Unauthorized, fwm.FailedPrecondition},
	}.Options(), true
}

// ---- facade (client side) ----

func (m *listManager) AddTodo(rc fwm.Context, owner string, wording string, requestID string) (ChangeOutcome, error) {
	in := m.input(rc, owner)
	in.Wording, in.RequestID = wording, requestID
	var out ChangeOutcome
	return out, m.run(rc, wfAdd, in, &out)
}

func (m *listManager) RewordTodo(rc fwm.Context, owner string, todoID string, wording string) (ChangeOutcome, error) {
	var out ChangeOutcome
	if todoID == "" {
		return out, facadeErr(kindInvalidRequest, "todoId is empty", "")
	}
	in := m.input(rc, owner)
	in.TodoID, in.Wording = todoID, wording
	return out, m.run(rc, wfReword, in, &out)
}

func (m *listManager) DiscardTodo(rc fwm.Context, owner string, todoID string) (ChangeOutcome, error) {
	var out ChangeOutcome
	if todoID == "" {
		return out, facadeErr(kindInvalidRequest, "todoId is empty", "")
	}
	in := m.input(rc, owner)
	in.TodoID = todoID
	return out, m.run(rc, wfDiscard, in, &out)
}

func (m *listManager) MarkTodo(rc fwm.Context, owner string, todoID string, completed bool) (ChangeOutcome, error) {
	var out ChangeOutcome
	if todoID == "" {
		return out, facadeErr(kindInvalidRequest, "todoId is empty", "")
	}
	in := m.input(rc, owner)
	in.TodoID, in.Completed = todoID, completed
	return out, m.run(rc, wfMark, in, &out)
}

func (m *listManager) ToggleAll(rc fwm.Context, owner string) (ChangeOutcome, error) {
	var out ChangeOutcome
	return out, m.run(rc, wfToggle, m.input(rc, owner), &out)
}

func (m *listManager) ClearCompleted(rc fwm.Context, owner string) (ChangeOutcome, error) {
	var out ChangeOutcome
	return out, m.run(rc, wfClear, m.input(rc, owner), &out)
}

func (m *listManager) OpenList(rc fwm.Context, owner string, view View) (ListView, error) {
	var out ListView
	switch view {
	case "all", "active", "completed":
	default:
		return out, facadeErr(kindInvalidRequest, "UnknownView: "+string(view), "")
	}
	in := m.input(rc, owner)
	in.View = view
	return out, m.run(rc, wfOpen, in, &out)
}

// input maps rc.Principal into the workflow input; an empty owner is the requester's own list.
func (m *listManager) input(rc fwm.Context, owner string) opInput {
	p := rc.Principal
	if owner == "" {
		owner = p.Subject
	}
	kind := "member"
	if p.Kind == security.PrincipalApplication {
		kind = "agent"
	}
	return opInput{
		Owner:     owner,
		Requester: requester{Kind: kind, Subject: p.Subject, Delegations: delegationsOf(p)},
	}
}

// delegationsOf reads the principal's delegation claims: a list of {grantor, intents}.
func delegationsOf(p security.Principal) []accessgranting.Delegation {
	raw, ok := p.Claims["delegations"]
	if !ok {
		return nil
	}
	items, _ := raw.([]any)
	var out []accessgranting.Delegation
	for _, it := range items {
		mm, _ := it.(map[string]any)
		if mm == nil {
			continue
		}
		d := accessgranting.Delegation{}
		d.Grantor, _ = mm["grantor"].(string)
		switch is := mm["intents"].(type) {
		case []any:
			for _, x := range is {
				if s, ok := x.(string); ok {
					d.Intents = append(d.Intents, accessgranting.Intent(s))
				}
			}
		case []string:
			for _, s := range is {
				d.Intents = append(d.Intents, accessgranting.Intent(s))
			}
		}
		out = append(out, d)
	}
	return out
}

func (m *listManager) run(rc fwm.Context, name string, in opInput, out any) error {
	ctx := rc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	opts := client.StartWorkflowOptions{
		ID:                       "list-" + randomHex(),
		TaskQueue:                TaskQueue,
		WorkflowExecutionTimeout: 2 * time.Minute,
	}
	run, err := m.client.ExecuteWorkflow(ctx, opts, name, in)
	if err != nil {
		return facadeErr(kindUnavailable, "start workflow failed", "")
	}
	if err := run.Get(ctx, out); err != nil {
		var app *temporal.ApplicationError
		if errors.As(err, &app) && app.Type() == listErrType {
			var le ListError
			if app.Details(&le) == nil {
				reason := ""
				if le.Reason != nil {
					reason = *le.Reason
				}
				return facadeErr(le.Kind, le.Message, reason)
			}
		}
		return facadeErr(kindUnavailable, "workflow failed", "")
	}
	return nil
}

// facadeErr maps a ListError kind onto the framework's Manager error model.
func facadeErr(kind, message, reason string) error {
	k := fwm.Infrastructure
	switch kind {
	case kindRefused:
		k = fwm.Unauthorized
	case kindTodoNotFound:
		k = fwm.NotFound
	case kindInvalidRequest:
		k = fwm.ContractMisuse
	}
	detail := kind + ": " + message
	if reason != "" {
		detail += " (" + reason + ")"
	}
	return fwm.New(k, detail)
}

func randomHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- workflows ----

func listFailure(kind, message, reason string) error {
	le := ListError{Kind: kind, Message: message}
	if reason != "" {
		le.Reason = &reason
	}
	return temporal.NewNonRetryableApplicationError(message, listErrType, nil, le)
}

// activityFailure classifies an activity error into a ListError failure.
func activityFailure(err error) error {
	var app *temporal.ApplicationError
	if errors.As(err, &app) {
		switch app.Type() {
		case fwm.RAErrType(fwra.NotFound):
			return listFailure(kindTodoNotFound, "todo not found", "")
		case fwm.RAErrType(fwra.ContractMisuse):
			return listFailure(kindInvalidRequest, "invalid request", "")
		}
	}
	return listFailure(kindUnavailable, "the list store is unavailable", "")
}

func engineFailure(err error) error {
	var ee *fweng.Error
	if errors.As(err, &ee) && ee.Kind == fweng.ContractMisuse && strings.HasPrefix(ee.Detail, "UnknownView") {
		return listFailure(kindInvalidRequest, ee.Detail, "")
	}
	return listFailure(kindUnavailable, "ruling unavailable", "")
}

func (m *listManager) ec(in opInput) fweng.Context {
	kind := security.PrincipalUser
	if in.Requester.Kind == "agent" {
		kind = security.PrincipalApplication
	}
	return fweng.Context{Context: context.Background(), Principal: security.Principal{Kind: kind, Subject: in.Requester.Subject}}
}

func invokers() genInvokers { return genInvokers{Opts: activityOptions} }

// admit asks AccessGrantingEngine first; a refusal stops the workflow before anything else.
func (m *listManager) admit(in opInput, intent accessgranting.Intent) error {
	d, err := m.access.DecideAccess(m.ec(in), accessgranting.AccessQuestion{
		Delegations:      in.Requester.Delegations,
		Intent:           intent,
		Owner:            in.Owner,
		RequesterKind:    accessgranting.RequesterKind(in.Requester.Kind),
		RequesterSubject: in.Requester.Subject,
	})
	if err != nil {
		return listFailure(kindUnavailable, "access decision unavailable", "")
	}
	if !d.Permitted {
		return listFailure(kindRefused, "access refused", string(d.Reason))
	}
	return nil
}

func ruled(l todolist.TodoList) []listruling.RuledTodo {
	out := make([]listruling.RuledTodo, 0, len(l.Todos))
	for _, t := range l.Todos {
		out = append(out, listruling.RuledTodo{Id: t.Id, Title: t.Title, Completed: t.Completed})
	}
	return out
}

func toTodo(t todolist.Todo) *Todo {
	return &Todo{Id: t.Id, Title: t.Title, Completed: t.Completed}
}

func (m *listManager) tally(in opInput, l todolist.TodoList) (int64, error) {
	n, err := m.ruling.TallyItemsLeft(m.ec(in), ruled(l))
	if err != nil {
		return 0, engineFailure(err)
	}
	return n.Count, nil
}

// flow carries the workflow context so shared steps need no workflow.Context parameter.
type flow struct {
	ctx workflow.Context
	m   *listManager
	in  opInput
}

// finish tallies the resulting list and publishes the change.
func (f flow) finish(outcome Outcome, todo *Todo, l todolist.TodoList, publish bool) (ChangeOutcome, error) {
	left, err := f.m.tally(f.in, l)
	if err != nil {
		return ChangeOutcome{}, err
	}
	if publish {
		// Publish is total: a kept change is never reported failed for propagation reasons.
		_ = invokers().MessageBusPublish(f.ctx, messagebus.ListChanged{Owner: f.in.Owner})
	}
	return ChangeOutcome{Outcome: outcome, Todo: todo, ItemsLeft: left}, nil
}

// recallThenFinish recalls the whole list after a keep, then tallies and publishes.
func (f flow) recallThenFinish(outcome Outcome, todo *Todo, publish bool) (ChangeOutcome, error) {
	l, err := invokers().TodoListAccessRecallList(f.ctx, f.in.Owner)
	if err != nil {
		return ChangeOutcome{}, activityFailure(err)
	}
	return f.finish(outcome, todo, l, publish)
}
