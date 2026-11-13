import { useEffect, useState } from 'react';
import type { ListViewName } from './useTodoList';

const KNOWN: Record<string, ListViewName> = {
  '': 'all',
  '#/': 'all',
  '#/active': 'active',
  '#/completed': 'completed',
};

/** The chosen view lives in the URL hash only; an unknown hash falls back to All. */
export function useHashView(): ListViewName {
  const [view, setView] = useState<ListViewName>(() => KNOWN[window.location.hash] ?? 'all');
  useEffect(() => {
    const onChange = (): void => {
      const known = KNOWN[window.location.hash];
      if (known === undefined) window.location.hash = '#/';
      setView(known ?? 'all');
    };
    window.addEventListener('hashchange', onChange);
    return (): void => {
      window.removeEventListener('hashchange', onChange);
    };
  }, []);
  return view;
}
