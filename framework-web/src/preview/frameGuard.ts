/**
 * The preview's frame guard. A frame is the way around the network guard: a
 * blank `<iframe>` (about:blank, which `frame-src 'none'` does not refuse) is a
 * same-origin window with its OWN, unguarded fetch, XMLHttpRequest, WebSocket,
 * navigator.sendBeacon and window.open. The inherited CSP still refuses its
 * network, silently; but window.open is not governed by any CSP, so its popup
 * would carry a URL out.
 *
 * So every child window is guarded the moment it can be reached:
 *
 *   - `guardChild` runs on it: the same network guard (so its channels throw
 *     and are reported, window.open included), the CSP-violation reporter, and
 *     this frame guard, for its own frames;
 *   - it is reached by index (`window[i]`), which covers every child browsing
 *     context however it was inserted, shadow roots included;
 *   - the sweep runs synchronously after every DOM insertion method, and
 *     inside the contentWindow / contentDocument getters, so
 *     `body.appendChild(f); f.contentWindow.fetch(…)` and `window[0].open(…)`
 *     already meet a guarded window;
 *   - a MutationObserver sweeps again as a backstop, for an insertion path not
 *     wrapped here; such a frame is guarded one microtask late, and its network
 *     is still refused by the inherited CSP.
 *
 * Every new frame is itself reported: a preview renders no frame (a nested
 * preview is off, and frame-src 'none' refuses any other), so one appearing is
 * a door being tried.
 */

/** Guards one newly reached child window. */
export type GuardChild = (child: Window) => void;

const INSERTION_METHODS: Readonly<Record<string, readonly string[]>> = {
  Node: ['appendChild', 'insertBefore', 'replaceChild'],
  Element: [
    'append',
    'prepend',
    'before',
    'after',
    'replaceWith',
    'replaceChildren',
    'insertAdjacentElement',
    'insertAdjacentHTML',
    'setHTMLUnsafe',
  ],
  Document: ['write', 'writeln'],
  Range: ['insertNode', 'surroundContents'],
};

const HTML_SETTERS: Readonly<Record<string, readonly string[]>> = {
  Element: ['innerHTML', 'outerHTML'],
  ShadowRoot: ['innerHTML'],
};

const FRAME_ELEMENTS = ['HTMLIFrameElement', 'HTMLFrameElement', 'HTMLObjectElement'];

type Ctor = { prototype: object } | undefined;

function ctor(win: Window, name: string): Ctor {
  return (win as unknown as Record<string, Ctor>)[name];
}

function describeFrame(child: Window): string {
  try {
    const el = child.frameElement;
    const src = el?.getAttribute('src') ?? el?.getAttribute('data');
    return src !== null && src !== undefined && src !== '' ? src : 'about:blank';
  } catch {
    return '(cross-origin frame)';
  }
}

/**
 * Installs the frame guard on `win`: each child window reached is passed to
 * `guardChild` once and reported through `onFrame`.
 */
export function installFrameGuard(
  win: Window,
  guardChild: GuardChild,
  onFrame: (detail: string) => void
): void {
  const seen = new WeakSet<Window>();
  const sweep = (): void => {
    for (let i = 0; i < win.length; i++) {
      const child = win[i];
      if (child === undefined || seen.has(child)) continue;
      seen.add(child);
      onFrame(describeFrame(child));
      try {
        guardChild(child);
      } catch {
        // A cross-origin child (a CSP-refused frame's error page) cannot be
        // patched, and cannot reach this page's data either.
      }
    }
  };

  for (const [name, methods] of Object.entries(INSERTION_METHODS)) {
    const proto = ctor(win, name)?.prototype as Record<string, unknown> | undefined;
    if (proto === undefined) continue;
    for (const method of methods) {
      const original = proto[method];
      if (typeof original !== 'function') continue;
      proto[method] = function (this: unknown, ...args: unknown[]): unknown {
        const result: unknown = Reflect.apply(original, this, args);
        sweep();
        return result;
      };
    }
  }

  const wrapAccessor = (proto: object, prop: string, kind: 'get' | 'set'): void => {
    const desc = Object.getOwnPropertyDescriptor(proto, prop);
    const original = desc?.[kind];
    if (desc === undefined || original === undefined) return;
    Object.defineProperty(proto, prop, {
      ...desc,
      [kind](this: unknown, ...args: unknown[]): unknown {
        if (kind === 'get') sweep();
        const result: unknown = Reflect.apply(original, this, args);
        sweep();
        return result;
      },
    });
  };
  for (const [name, props] of Object.entries(HTML_SETTERS)) {
    const proto = ctor(win, name)?.prototype;
    if (proto !== undefined) for (const prop of props) wrapAccessor(proto, prop, 'set');
  }
  for (const name of FRAME_ELEMENTS) {
    const proto = ctor(win, name)?.prototype;
    if (proto === undefined) continue;
    wrapAccessor(proto, 'contentWindow', 'get');
    wrapAccessor(proto, 'contentDocument', 'get');
  }

  const realm = win as Window & typeof globalThis;
  new realm.MutationObserver(sweep).observe(win.document, { childList: true, subtree: true });
  sweep();
}
