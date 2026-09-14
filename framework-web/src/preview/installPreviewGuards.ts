/**
 * Installs the preview's guards on the page:
 *
 *   - the network guard (networkGuard.ts): fetch, XHR, WebSocket, EventSource,
 *     sendBeacon and window.open throw instead of reaching anything;
 *   - the navigation guard (navigationGuard.ts): a link or navigation that
 *     would open another tab or window, leave for another origin, or reload the
 *     preview is refused where the browser lets a page refuse it;
 *   - the frame guard (frameGuard.ts): every frame is reported, and its window
 *     gets these same guards before a script can use it;
 *   - the CSP reporter: whatever the page's CSP refuses (an image, a script, a
 *     font, a prefetch, a form submission…) is reported, in the page and in
 *     every frame, so a refusal is never silent;
 *   - the session gate's signal: a 401 announced by the app's session probe
 *     (framework-web's announceUnauthenticated) is refused a reload and
 *     reported; PreviewSessionGate then shows the preview error page.
 *
 * Every refusal is raised as a preview incident, so it shows on the alarm. The
 * preview entry imports `…/preview/install` FIRST, which calls this before any
 * other module evaluates.
 */
import { UNAUTHENTICATED_EVENT } from '../context/sessionEvents.ts';
import { installFrameGuard } from './frameGuard.ts';
import { installNavigationGuard } from './navigationGuard.ts';
import { installNetworkGuard } from './networkGuard.ts';
import { raisePreviewIncident } from './previewIncidents.ts';

/** The guards every window of the preview gets: the page and each frame. */
function guardWindow(win: Window): void {
  installNetworkGuard(win, (error) => {
    raisePreviewIncident({ kind: 'network-blocked', detail: `${error.channel} ${error.target}` });
  });
  win.document.addEventListener('securitypolicyviolation', (event) => {
    raisePreviewIncident({
      kind: 'csp-violation',
      detail: `${event.effectiveDirective} ${event.blockedURI}`,
    });
  });
  installFrameGuard(win, guardWindow, (detail) => {
    raisePreviewIncident({ kind: 'frame-blocked', detail });
  });
}

export function installPreviewGuards(win: Window = window): void {
  win.__ARCHISTRATOR_PREVIEW__ = { incidents: [] };
  guardWindow(win);
  installNavigationGuard(win, (href) => {
    raisePreviewIncident({ kind: 'navigation-blocked', detail: href });
  });
  win.addEventListener(UNAUTHENTICATED_EVENT, (event) => {
    event.preventDefault();
    raisePreviewIncident({
      kind: 'unauthenticated',
      detail: 'the session probe answered 401; a preview cannot sign in',
    });
  });
}
