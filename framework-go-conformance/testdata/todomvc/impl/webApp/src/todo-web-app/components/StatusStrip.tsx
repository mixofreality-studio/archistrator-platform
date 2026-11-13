import type { JSX } from 'react';
import type { Notice } from '../hooks/useTodoList';

const TEXT: Record<Notice['kind'], string> = {
  refused: 'That change is outside your reach — nothing was changed.',
  gone: 'That todo was removed elsewhere.',
  unavailable: 'Couldn’t reach your list — nothing was changed.',
};

export function StatusStrip({
  notice,
  onRetry,
}: {
  notice: Notice | null;
  onRetry: () => void;
}): JSX.Element {
  return (
    <div aria-label="Notice" aria-live="polite" className="status-strip" data-testid="status-strip" role="status">
      {notice !== null && (
        <>
          <span>{TEXT[notice.kind]}</span>
          {notice.retryable ? (
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          ) : null}
        </>
      )}
    </div>
  );
}
