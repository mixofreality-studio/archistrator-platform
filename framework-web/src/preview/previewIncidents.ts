/**
 * The preview's incident log: every fixture miss and every refused or reported
 * door. It is how a preview fails LOUDLY (design-renderer-data.md §2′.1):
 *
 *   - each incident is logged with console.error;
 *   - the PreviewAlarm banner renders the log over the app;
 *   - `window.__ARCHISTRATOR_PREVIEW__.incidents` exposes it to a headless
 *     reader: an app's UI tests, and the rig's smoke gate (§2′.2 step 3), which
 *     fails a state on any GET miss.
 *
 * The kinds:
 *   - fixture-miss: an op the state's fixture does not answer;
 *   - network-blocked: a request the network guard refused (fetch, XHR,
 *     WebSocket, EventSource, sendBeacon, window.open), in the page or a frame;
 *   - navigation-blocked: a link or navigation that would open another tab or
 *     window, leave for another origin, or reload the preview;
 *   - frame-blocked: a frame appeared; a preview renders none;
 *   - csp-violation: the page's CSP refused something (an image, a script, a
 *     font, a prefetch, a form submission…) from another origin;
 *   - unauthenticated: the app's session probe answered 401, which a preview
 *     cannot sign in to answer.
 *
 * A plain module store (subscribe + snapshot) so React can read it through
 * useSyncExternalStore and non-React code (the guards, the transport's onMiss)
 * can write it.
 */

export type PreviewIncidentKind =
  | 'fixture-miss'
  | 'network-blocked'
  | 'navigation-blocked'
  | 'frame-blocked'
  | 'csp-violation'
  | 'unauthenticated';

export interface PreviewIncident {
  readonly kind: PreviewIncidentKind;
  readonly detail: string;
}

declare global {
  interface Window {
    __ARCHISTRATOR_PREVIEW__?: { readonly incidents: readonly PreviewIncident[] };
  }
}

let incidents: readonly PreviewIncident[] = [];
const listeners = new Set<() => void>();

export function raisePreviewIncident(incident: PreviewIncident): void {
  incidents = [...incidents, incident];
  console.error(`[archistrator preview] ${incident.kind}: ${incident.detail}`);
  if (typeof window !== 'undefined') {
    window.__ARCHISTRATOR_PREVIEW__ = { incidents };
  }
  for (const listener of listeners) listener();
}

export function subscribePreviewIncidents(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function previewIncidents(): readonly PreviewIncident[] {
  return incidents;
}
