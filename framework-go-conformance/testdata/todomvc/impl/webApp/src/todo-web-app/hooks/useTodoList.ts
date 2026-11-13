import { useCallback, useEffect, useRef, useState } from 'react';
import { useOpsClient } from '../../api/opsContext';
import type { OpBody, OpResult } from '../../api/opTypes';
import { ApiError } from '../../contracts/errors';

export type ListViewName = OpBody<'listOpenList'>['view'];
export type ListSnapshot = OpResult<'listOpenList'>;
export type ChangeOutcome = OpResult<'listAddTodo'>;
export type Todo = ListSnapshot['todos'][number];

export type Phase = 'loading' | 'ready' | 'refused' | 'unavailable';
export interface Notice {
  kind: 'refused' | 'gone' | 'unavailable';
  /** True when Retry repeats the same op (same request id). */
  retryable: boolean;
}

export interface TodoListApi {
  phase: Phase;
  list: ListSnapshot | null;
  notice: Notice | null;
  busy: ReadonlySet<string>;
  reload: () => void;
  retry: () => void;
  addTodo: (wording: string) => Promise<boolean>;
  rewordTodo: (todoId: string, wording: string) => Promise<boolean>;
  discardTodo: (todoId: string) => Promise<boolean>;
  markTodo: (todoId: string, completed: boolean) => Promise<boolean>;
  toggleAll: () => Promise<boolean>;
  clearCompleted: () => Promise<boolean>;
}

const NOTICE_MS = 6000;
// An empty owner is the requester's own list (ListManager contract).
const OWNER = '';
const ADD_KEY = '\u0000add';
const BULK_KEY = '\u0000bulk';

type ChangeOp =
  | 'listRewordTodo'
  | 'listDiscardTodo'
  | 'listMarkTodo'
  | 'listToggleAll'
  | 'listClearCompleted'
  | 'listAddTodo';

function noticeKind(err: unknown): Notice['kind'] {
  if (err instanceof ApiError) {
    if (err.status === 403) return 'refused';
    if (err.status === 404) return 'gone';
  }
  return 'unavailable';
}

/** The ListClient adapter: the one place the surface calls ListManager. Holds no list rules. */
export function useTodoList(view: ListViewName): TodoListApi {
  const { ops } = useOpsClient();
  const [phase, setPhase] = useState<Phase>('loading');
  const [list, setList] = useState<ListSnapshot | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const viewRef = useRef(view);
  const seq = useRef(0);
  const loaded = useRef(false);
  const retryRef = useRef<() => void>(() => undefined);
  const openRef = useRef<(target: ListViewName) => Promise<void>>(() => Promise.resolve());
  const performRef = useRef<(key: string, call: () => Promise<ChangeOutcome>) => Promise<boolean>>(
    () => Promise.resolve(false)
  );

  const open = useCallback(
    async (target: ListViewName): Promise<void> => {
      const mine = ++seq.current;
      try {
        const next = await ops.callForBody<ListSnapshot>('listOpenList', {
          body: { owner: OWNER, view: target } satisfies OpBody<'listOpenList'>,
        });
        if (mine !== seq.current) return;
        loaded.current = true;
        setList(next);
        setPhase('ready');
      } catch (err) {
        if (mine !== seq.current) return;
        if (noticeKind(err) === 'refused') {
          loaded.current = false;
          setList(null);
          setPhase('refused');
        } else if (!loaded.current) {
          setPhase('unavailable');
        } else {
          retryRef.current = (): void => {
            void openRef.current(viewRef.current);
          };
          setNotice({ kind: 'unavailable', retryable: true });
        }
      }
    },
    [ops]
  );

  useEffect(() => {
    openRef.current = open;
  }, [open]);

  useEffect(() => {
    viewRef.current = view;
    // Fetching on view change is the effect's whole purpose; state is set after the await.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void open(view);
  }, [view, open]);

  useEffect(() => {
    if (notice === null || notice.kind === 'unavailable') return undefined;
    const t = window.setTimeout(() => {
      setNotice(null);
    }, NOTICE_MS);
    return (): void => {
      window.clearTimeout(t);
    };
  }, [notice]);

  const perform = useCallback(
    async (key: string, call: () => Promise<ChangeOutcome>): Promise<boolean> => {
      setNotice(null);
      setBusy((b) => new Set(b).add(key));
      try {
        await call();
        await open(viewRef.current);
        return true;
      } catch (err) {
        const kind = noticeKind(err);
        if (kind === 'gone') {
          setNotice({ kind, retryable: false });
          await open(viewRef.current);
        } else if (kind === 'refused') {
          setNotice({ kind, retryable: false });
        } else {
          // Retry repeats the same op with the same request id.
          retryRef.current = (): void => {
            void performRef.current(key, call);
          };
          setNotice({ kind: 'unavailable', retryable: true });
        }
        return false;
      } finally {
        setBusy((b) => {
          const n = new Set(b);
          n.delete(key);
          return n;
        });
      }
    },
    [open]
  );

  useEffect(() => {
    performRef.current = perform;
  }, [perform]);

  const send = useCallback(
    <K extends ChangeOp>(op: K, body: OpBody<K>): Promise<ChangeOutcome> =>
      ops.callForBody<ChangeOutcome>(op, { body }),
    [ops]
  );

  return {
    phase,
    list,
    notice,
    busy,
    reload: (): void => {
      setPhase('loading');
      void open(viewRef.current);
    },
    retry: (): void => {
      retryRef.current();
    },
    addTodo: (wording: string): Promise<boolean> => {
      const requestId = crypto.randomUUID();
      return perform(ADD_KEY, () => send('listAddTodo', { owner: OWNER, wording, requestId }));
    },
    rewordTodo: (todoId: string, wording: string): Promise<boolean> =>
      perform(todoId, () => send('listRewordTodo', { owner: OWNER, todoId, wording })),
    discardTodo: (todoId: string): Promise<boolean> =>
      perform(todoId, () => send('listDiscardTodo', { owner: OWNER, todoId })),
    markTodo: (todoId: string, completed: boolean): Promise<boolean> =>
      perform(todoId, () => send('listMarkTodo', { owner: OWNER, todoId, completed })),
    toggleAll: (): Promise<boolean> => perform(BULK_KEY, () => send('listToggleAll', { owner: OWNER })),
    clearCompleted: (): Promise<boolean> =>
      perform(BULK_KEY, () => send('listClearCompleted', { owner: OWNER })),
  };
}
