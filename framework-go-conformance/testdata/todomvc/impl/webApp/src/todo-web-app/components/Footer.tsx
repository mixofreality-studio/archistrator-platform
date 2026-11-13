import type { JSX } from 'react';
import type { ListViewName } from '../hooks/useTodoList';

const FILTERS: { view: ListViewName; href: string; label: string }[] = [
  { view: 'all', href: '#/', label: 'All' },
  { view: 'active', href: '#/active', label: 'Active' },
  { view: 'completed', href: '#/completed', label: 'Completed' },
];

export function Footer({
  itemsLeft,
  completedCount,
  view,
  disabled,
  onClear,
}: {
  itemsLeft: number;
  completedCount: number;
  view: ListViewName;
  disabled: boolean;
  onClear: () => void;
}): JSX.Element {
  return (
    <footer className="footer">
      <span aria-label="Items left" className="todo-count" data-testid="items-left" role="status">
        <strong>{itemsLeft}</strong> {itemsLeft === 1 ? 'item' : 'items'} left
      </span>
      <ul className="filters">
        {FILTERS.map((f) => (
          <li key={f.view}>
            <a
              aria-current={f.view === view ? 'page' : undefined}
              className={f.view === view ? 'selected' : ''}
              data-testid={`filter-${f.view}`}
              href={f.href}
            >
              {f.label}
            </a>
          </li>
        ))}
      </ul>
      {completedCount > 0 && (
        <button
          className="clear-completed"
          data-testid="clear-completed"
          disabled={disabled}
          type="button"
          onClick={onClear}
        >
          Clear completed
        </button>
      )}
    </footer>
  );
}
