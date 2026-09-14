/**
 * The preview-mode runtime (`@mixofreality-studio/archistrator-platform-framework-web/preview`):
 * an app's real shell and components, booted over fixture data with every other
 * route to the network closed (design-renderer-data.md §2′.1). Only a preview
 * entry may import it; the production bundle check fails a bundle that carries it.
 *
 * Node-side tooling (the bundle check, the fixture validator and the Vite plugin)
 * is `…/preview/node`; the guards' side-effect entry is `…/preview/install`.
 */
export {
  FIXTURE_MISS_CODE,
  createFixtureTransport,
  type ApiErrorClass,
  type FixtureError,
  type FixtureFile,
  type FixtureMissErrorClass,
  type FixtureOp,
  type FixtureOpParams,
  type FixtureOps,
  type FixtureOpsClient,
  type FixtureOpsOptions,
  type FixtureTransport,
  type FixtureTransportConfig,
  type OpRoute,
} from './fixtureOps.ts';
export {
  NETWORK_GUARD_MARKER,
  PreviewNetworkBlockedError,
  installNetworkGuard,
} from './networkGuard.ts';
export {
  installNavigationGuard,
  leavesOrigin,
  opensNewContext,
  type LinkActivation,
} from './navigationGuard.ts';
export {
  previewIncidents,
  raisePreviewIncident,
  subscribePreviewIncidents,
  type PreviewIncident,
  type PreviewIncidentKind,
} from './previewIncidents.ts';
export { installPreviewGuards } from './installPreviewGuards.ts';
export {
  resolvePreviewState,
  statesFromModules,
  type PreviewResolution,
  type PreviewScreenListing,
  type PreviewStates,
  type UnresolvedPreview,
} from './fixtureRegistry.ts';
export { PreviewAlarm, type PreviewAlarmProps } from './PreviewAlarm.tsx';
export {
  PreviewErrorPage,
  type PreviewErrorPageProps,
  type PreviewFailure,
  type UnauthenticatedPreview,
} from './PreviewErrorPage.tsx';
export { PreviewSessionGate, type PreviewSessionGateProps } from './PreviewSessionGate.tsx';
export { installFrameGuard, type GuardChild } from './frameGuard.ts';
