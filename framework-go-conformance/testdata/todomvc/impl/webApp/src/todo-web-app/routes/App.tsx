import type { JSX } from 'react';
import { Footer } from '../components/Footer';
import { NewTodoInput } from '../components/NewTodoInput';
import { StatusStrip } from '../components/StatusStrip';
import { TodoItem } from '../components/TodoItem';
import { useHashView } from '../hooks/useHashView';
import { useTodoList, type ListViewName } from '../hooks/useTodoList';

// The UI design names the list by the view it shows: unnamed on all, "Active todos", "Completed todos".
const LIST_NAME: Record<ListViewName, string | undefined> = {
  all: undefined,
  active: 'Active todos',
  completed: 'Completed todos',
};

export function App(): JSX.Element {
  const view = useHashView();
  const api = useTodoList(view);
  const { phase, list, busy } = api;

  let body: JSX.Element;
  if (phase === 'refused') {
    body = (
      <p aria-label="List refused" className="message" data-testid="list-refused" role="status">
        This list is outside your reach.
      </p>
    );
  } else if (phase === 'unavailable') {
    body = (
      <p className="message">
        Couldn’t reach your list — nothing was changed.{' '}
        <button type="button" onClick={api.reload}>
          Retry
        </button>
      </p>
    );
  } else if (phase === 'loading' || list === null) {
    body = <p className="message">Loading your list…</p>;
  } else {
    // Presentation only: derived as the contract documents; never used to decide an outcome.
    const completedCount = list.totalCount - list.itemsLeft;
    body = (
      <>
        <header className="header-input">
          <NewTodoInput disabled={busy.size > 0} onAdd={api.addTodo} />
        </header>
        {list.totalCount > 0 && (
          <section className="main">
            <input
              checked={list.itemsLeft === 0}
              className="toggle-all"
              data-testid="toggle-all"
              disabled={busy.size > 0}
              id="toggle-all"
              type="checkbox"
              onChange={() => {
                void api.toggleAll();
              }}
            />
            <label htmlFor="toggle-all">Mark all as complete</label>
            <ul aria-label={LIST_NAME[view]} className="todo-list">
              {list.todos.map((t) => (
                <TodoItem
                  busy={busy.has(t.id)}
                  key={t.id}
                  todo={t}
                  onDiscard={() => {
                    void api.discardTodo(t.id);
                  }}
                  onMark={(c) => {
                    void api.markTodo(t.id, c);
                  }}
                  onReword={(w) => api.rewordTodo(t.id, w)}
                />
              ))}
            </ul>
          </section>
        )}
        {list.totalCount > 0 && (
          <Footer
            completedCount={completedCount}
            disabled={busy.size > 0}
            itemsLeft={list.itemsLeft}
            view={view}
            onClear={() => {
              void api.clearCompleted();
            }}
          />
        )}
      </>
    );
  }

  return (
    <main className="todoapp">
      <h1>todos</h1>
      <StatusStrip notice={api.notice} onRetry={api.retry} />
      {body}
    </main>
  );
}
