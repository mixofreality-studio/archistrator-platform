/**
 * The preview's navigation guard. A preview stays one document, in one tab, on
 * its own origin:
 *
 *   - a NEW TAB or window is refused: an "Open in a new tab" link would boot a
 *     second copy of the app from the preview's origin (a nested preview, off
 *     inside a preview: design-renderer-data.md §2′.5 item 5), and any other
 *     `target="_blank"` link would leave the fixture world. A frame is refused
 *     by the CSP (`frame-src 'none'`) and window.open by the network guard;
 *   - a SAME-TAB exit to another origin is refused: a plain click on a link to
 *     another origin, and, where the browser has the Navigation API, any
 *     navigation of this document to another origin (location.href,
 *     location.assign/replace, a meta refresh, a form's GET) and any scripted
 *     reload (a 401 handler's `location.reload()` would loop forever over the
 *     same fixture).
 *
 * The honest limit: no CSP directive governs navigation, so this guard is the
 * only layer. Without the Navigation API (Chromium has it; other engines are
 * shipping it) a scripted same-tab navigation to another origin is NOT caught,
 * and a navigation the browser does not let a page cancel is reported but
 * still happens. What such a navigation can carry out is the fixture data in
 * its URL: the preview origin holds no cookies and no secrets (§2′.3), so that
 * residual is accepted, and every navigation the guard sees is reported loudly.
 *
 * In-app links are untouched: the router's own links are same-origin and are
 * handled on the memory history, and a same-origin navigation (the error
 * page's state links) is allowed. The decisions are pure functions
 * (opensNewContext, leavesOrigin), unit-tested under node; the listeners are
 * thin.
 */

export interface LinkActivation {
  /** The anchor's `target` attribute ('' when absent). */
  readonly target: string;
  /** MouseEvent.button: 0 primary, 1 middle. */
  readonly button: number;
  readonly ctrlKey: boolean;
  readonly metaKey: boolean;
  readonly shiftKey: boolean;
}

/** True when activating the link would open another tab or window. */
export function opensNewContext(a: LinkActivation): boolean {
  const target = a.target.trim().toLowerCase();
  if (target !== '' && target !== '_self') return true;
  if (a.button === 1) return true;
  return a.ctrlKey || a.metaKey || a.shiftKey;
}

/**
 * True when `href`, resolved against `base`, is on another origin than `base`.
 * A URL that does not parse counts as leaving, and so does an opaque one
 * (`javascript:`, `data:`), whose origin is "null": the guard refuses what it
 * cannot place.
 */
export function leavesOrigin(href: string, base: string): boolean {
  let url: URL;
  try {
    url = new URL(href, base);
  } catch {
    return true;
  }
  return url.origin !== new URL(base).origin;
}

/** The part of the Navigation API the guard uses. */
interface NavigateEventLike extends Event {
  readonly navigationType: string;
  readonly destination: { readonly url: string };
}

interface NavigationLike {
  addEventListener(type: 'navigate', listener: (event: NavigateEventLike) => void): void;
}

/**
 * Refuses, in the capture phase (before any app handler), every link activation
 * that would open another browsing context or leave for another origin, and,
 * through the Navigation API where present, every navigation of the document to
 * another origin and every scripted reload. `onBlocked` hears the destination.
 */
export function installNavigationGuard(win: Window, onBlocked: (href: string) => void): void {
  // The page's own realm: a frame's elements are instances of ITS constructors.
  const realm = win as Window & typeof globalThis;
  const guard = (event: MouseEvent): void => {
    const origin = event.target instanceof realm.Element ? event.target : null;
    const link = origin?.closest('a[href], area[href]');
    if (!(link instanceof realm.HTMLAnchorElement || link instanceof realm.HTMLAreaElement)) return;
    // Read each field off the event: its button and modifier flags are prototype
    // getters, which a spread would silently drop.
    const activation: LinkActivation = {
      target: link.target,
      button: event.button,
      ctrlKey: event.ctrlKey,
      metaKey: event.metaKey,
      shiftKey: event.shiftKey,
    };
    if (!opensNewContext(activation) && !leavesOrigin(link.href, win.location.href)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    onBlocked(link.href);
  };
  win.document.addEventListener('click', guard, { capture: true });
  win.document.addEventListener('auxclick', guard, { capture: true });

  const navigation = (win as unknown as { navigation?: NavigationLike }).navigation;
  navigation?.addEventListener('navigate', (event) => {
    const url = event.destination.url;
    const reload = event.navigationType === 'reload';
    if (!reload && !leavesOrigin(url, win.location.href)) return;
    if (event.cancelable) event.preventDefault();
    onBlocked(`${reload ? 'reload ' : ''}${url}${event.cancelable ? '' : ' (not cancelable)'}`);
  });
}
