// The UI design's todo-title is a heading that is also the double-click target of a reword, so the
// heading carries the handlers and a tab stop (see README, "Acceptance plan and the harvested SPA").
/* eslint-disable jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex */
import { useEffect, useRef, useState, type JSX } from 'react';
import type { Todo } from '../hooks/useTodoList';

export function TodoItem({
  todo,
  busy,
  onMark,
  onReword,
  onDiscard,
}: {
  todo: Todo;
  busy: boolean;
  onMark: (completed: boolean) => void;
  onReword: (wording: string) => Promise<boolean>;
  onDiscard: () => void;
}): JSX.Element {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(todo.title);
  const editRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const el = editRef.current;
    if (editing && el !== null) {
      el.focus();
      el.setSelectionRange(el.value.length, el.value.length);
    }
  }, [editing]);

  const startEdit = (): void => {
    if (busy) return;
    setDraft(todo.title);
    setEditing(true);
  };
  const commit = (): void => {
    if (!editing || busy) return;
    void onReword(draft).then((ok) => {
      if (ok) setEditing(false);
    });
  };

  const cls = [todo.completed ? 'completed' : '', editing ? 'editing' : '', busy ? 'busy' : '']
    .filter(Boolean)
    .join(' ');

  return (
    <li aria-busy={busy} aria-label="Todo" className={cls} data-testid="todo-item">
      {editing ? (
        <input
          aria-label="Edit todo"
          className="edit"
          data-testid="todo-edit-input"
          ref={editRef}
          value={draft}
          onBlur={commit}
          onChange={(e) => {
            setDraft(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commit();
            if (e.key === 'Escape') setEditing(false);
          }}
        />
      ) : (
        <div className="view">
          <input
            aria-label="Completed"
            checked={todo.completed}
            className="toggle"
            data-testid="todo-checkbox"
            disabled={busy}
            type="checkbox"
            onChange={() => {
              onMark(!todo.completed);
            }}
          />
          <h2
            aria-label="Todo title"
            className="title"
            data-testid="todo-title"
            tabIndex={0}
            onDoubleClick={startEdit}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === 'F2') {
                e.preventDefault();
                startEdit();
              }
            }}
          >
            {todo.title}
          </h2>
          <button
            aria-label="Delete"
            className="destroy"
            data-testid="todo-destroy"
            disabled={busy}
            type="button"
            onClick={onDiscard}
          >
            ×
          </button>
        </div>
      )}
    </li>
  );
}
