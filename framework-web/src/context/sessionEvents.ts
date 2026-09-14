/**
 * The session-probe signal. When an app's session probe answers 401, the app
 * would reload so the edge issues the OIDC redirect. It announces the 401 first,
 * as a cancelable event on the window, and reloads only when nothing claimed it.
 *
 * A preview claims it (framework-web's preview guards): a preview cannot sign
 * in, and a reload would answer the same fixture 401 forever, so it shows the
 * preview error page instead. An app with its own session provider calls
 * announceUnauthenticated the same way.
 */

/** Dispatched on the window, cancelable, when the session probe answers 401. */
export const UNAUTHENTICATED_EVENT = 'archistrator:unauthenticated';

/**
 * Announces a 401. Returns true when nothing claimed it, so the caller should
 * reload to sign in; false when a listener (a preview) took it over.
 */
export function announceUnauthenticated(win: Window = window): boolean {
  return win.dispatchEvent(new Event(UNAUTHENTICATED_EVENT, { cancelable: true }));
}
