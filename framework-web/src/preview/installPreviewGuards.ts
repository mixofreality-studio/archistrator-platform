/**
 * Installs the preview's guards on the page:
 *
 *   - the network guard (networkGuard.ts): fetch, XHR, WebSocket, EventSource,
 *     sendBeacon and window.open throw instead of reaching anything;
 *   - the navigation guard (navigationGuard.ts): a link that would open another
 *     tab or window, a nested preview among them, is refused.
 *
 * Every refusal is raised as a preview incident, so it shows on the alarm. The
 * preview entry imports `…/preview/install` FIRST, which calls this before any
 * other module evaluates.
 */
import { installNavigationGuard } from './navigationGuard.ts';
import { installNetworkGuard } from './networkGuard.ts';
import { raisePreviewIncident } from './previewIncidents.ts';

export function installPreviewGuards(win: Window = window): void {
  win.__ARCHISTRATOR_PREVIEW__ = { incidents: [] };

  installNetworkGuard(win, (error) => {
    raisePreviewIncident({ kind: 'network-blocked', detail: `${error.channel} ${error.target}` });
  });

  installNavigationGuard(win.document, (href) => {
    raisePreviewIncident({ kind: 'navigation-blocked', detail: href });
  });
}
