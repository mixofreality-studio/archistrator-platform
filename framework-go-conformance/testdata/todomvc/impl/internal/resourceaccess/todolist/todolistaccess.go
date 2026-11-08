// Package todolist is the Postgres-backed ResourceAccess for the team-held todo list.
package todolist

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// Contract error kinds (AccessError.kind), carried in the error detail and mapped
// onto the framework's shared ResourceAccess error model:
//
//	TodoNotFound     -> fwra.NotFound       (terminal)
//	InvalidRequest   -> fwra.ContractMisuse (terminal)
//	StoreUnavailable -> fwra.Infrastructure (transient, retryable)
const (
	kindTodoNotFound     = "TodoNotFound"
	kindInvalidRequest   = "InvalidRequest"
	kindStoreUnavailable = "StoreUnavailable"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS todo_list_entry (
	seq       BIGSERIAL PRIMARY KEY,
	todo_id   TEXT    NOT NULL UNIQUE,
	owner     TEXT    NOT NULL,
	title     TEXT    NOT NULL,
	completed BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS todo_list_entry_owner_seq ON todo_list_entry (owner, seq);
`

type postgresTodoListAccess struct {
	pool *pgxpool.Pool
}

func newPostgresTodoListAccess(ctx context.Context, pool *pgxpool.Pool) (TodoListAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, kindInvalidRequest+": nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, unavailable(err, "ensure schema")
	}
	return &postgresTodoListAccess{pool: pool}, nil
}

func unavailable(err error, op string) error {
	return fwra.Wrap(fwra.Infrastructure, err, kindStoreUnavailable+": "+op)
}

func invalid(detail string) error {
	return fwra.New(fwra.ContractMisuse, kindInvalidRequest+": "+detail)
}

func notFound(detail string) error {
	return fwra.New(fwra.NotFound, kindTodoNotFound+": "+detail)
}

func (a *postgresTodoListAccess) RecallList(rc fwra.Context, owner string) (TodoList, error) {
	if owner == "" {
		return TodoList{}, invalid("empty owner")
	}
	return a.readList(rc, a.pool, owner, "RecallList")
}

func (a *postgresTodoListAccess) readList(ctx context.Context, q listReader, owner, op string) (TodoList, error) {
	rows, err := q.Query(ctx,
		`SELECT todo_id, title, completed FROM todo_list_entry WHERE owner = $1 ORDER BY seq`, owner)
	if err != nil {
		return TodoList{}, unavailable(err, op)
	}
	defer rows.Close()
	list := TodoList{Owner: owner, Todos: []Todo{}}
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.Id, &t.Title, &t.Completed); err != nil {
			return TodoList{}, unavailable(err, op)
		}
		list.Todos = append(list.Todos, t)
	}
	if err := rows.Err(); err != nil {
		return TodoList{}, unavailable(err, op)
	}
	return list, nil
}

func (a *postgresTodoListAccess) RecordTodo(rc fwra.Context, owner string, todoID string, title string) (Todo, error) {
	if owner == "" || todoID == "" || title == "" {
		return Todo{}, invalid("empty owner, todoID or title")
	}
	const op = "RecordTodo"
	if _, err := a.pool.Exec(rc,
		`INSERT INTO todo_list_entry (todo_id, owner, title, completed) VALUES ($1, $2, $3, FALSE)
		 ON CONFLICT (todo_id) DO NOTHING`, todoID, owner, title); err != nil {
		return Todo{}, unavailable(err, op)
	}
	var t Todo
	var gotOwner string
	err := a.pool.QueryRow(rc,
		`SELECT owner, todo_id, title, completed FROM todo_list_entry WHERE todo_id = $1`, todoID).
		Scan(&gotOwner, &t.Id, &t.Title, &t.Completed)
	if err != nil {
		return Todo{}, unavailable(err, op)
	}
	if gotOwner != owner {
		return Todo{}, invalid("todoID already recorded for a different owner")
	}
	return t, nil
}

func (a *postgresTodoListAccess) RetitleTodo(rc fwra.Context, owner string, todoID string, title string) (Todo, error) {
	if owner == "" || todoID == "" || title == "" {
		return Todo{}, invalid("empty owner, todoID or title")
	}
	var t Todo
	err := a.pool.QueryRow(rc,
		`UPDATE todo_list_entry SET title = $3 WHERE owner = $1 AND todo_id = $2
		 RETURNING todo_id, title, completed`, owner, todoID, title).
		Scan(&t.Id, &t.Title, &t.Completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return Todo{}, notFound("no such todo in the owner's list")
	}
	if err != nil {
		return Todo{}, unavailable(err, "RetitleTodo")
	}
	return t, nil
}

func (a *postgresTodoListAccess) RemoveTodo(rc fwra.Context, owner string, todoID string) (bool, error) {
	if owner == "" || todoID == "" {
		return false, invalid("empty owner or todoID")
	}
	tag, err := a.pool.Exec(rc,
		`DELETE FROM todo_list_entry WHERE owner = $1 AND todo_id = $2`, owner, todoID)
	if err != nil {
		return false, unavailable(err, "RemoveTodo")
	}
	return tag.RowsAffected() > 0, nil
}

func (a *postgresTodoListAccess) SetCompletion(rc fwra.Context, owner string, todoID string, completed bool) (Todo, error) {
	if owner == "" || todoID == "" {
		return Todo{}, invalid("empty owner or todoID")
	}
	var t Todo
	err := a.pool.QueryRow(rc,
		`UPDATE todo_list_entry SET completed = $3 WHERE owner = $1 AND todo_id = $2
		 RETURNING todo_id, title, completed`, owner, todoID, completed).
		Scan(&t.Id, &t.Title, &t.Completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return Todo{}, notFound("no such todo in the owner's list")
	}
	if err != nil {
		return Todo{}, unavailable(err, "SetCompletion")
	}
	return t, nil
}

func (a *postgresTodoListAccess) SetAllCompletion(rc fwra.Context, owner string, completed bool) (TodoList, error) {
	if owner == "" {
		return TodoList{}, invalid("empty owner")
	}
	return a.mutateList(rc, owner, "SetAllCompletion",
		`UPDATE todo_list_entry SET completed = $2 WHERE owner = $1 AND completed <> $2`, completed)
}

func (a *postgresTodoListAccess) PurgeCompleted(rc fwra.Context, owner string) (TodoList, error) {
	if owner == "" {
		return TodoList{}, invalid("empty owner")
	}
	return a.mutateList(rc, owner, "PurgeCompleted",
		`DELETE FROM todo_list_entry WHERE owner = $1 AND completed`)
}

// mutateList runs one bulk statement and reads the resulting list in the same
// transaction, so the write is atomic and the returned list is consistent with it.
func (a *postgresTodoListAccess) mutateList(rc fwra.Context, owner, op, stmt string, extra ...any) (TodoList, error) {
	tx, err := a.pool.Begin(rc)
	if err != nil {
		return TodoList{}, unavailable(err, op)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(rc)) }()
	args := append([]any{owner}, extra...)
	if _, err := tx.Exec(rc, stmt, args...); err != nil {
		return TodoList{}, unavailable(err, op)
	}
	list, err := a.readList(rc, tx, owner, op)
	if err != nil {
		return TodoList{}, err
	}
	if err := tx.Commit(rc); err != nil {
		return TodoList{}, unavailable(err, op)
	}
	return list, nil
}

// listReader is the query surface shared by the pool and a transaction.
type listReader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
