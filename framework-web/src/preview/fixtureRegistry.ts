/**
 * Which fixture a preview boots: `?screen=<id>&state=<id>` against the fixture
 * files bundled into the build (design-renderer-data.md §2′.1).
 *
 * The files are laid out `<root>/<surface>/<screenId>/<stateId>.json` and bundled
 * at build time through import.meta.glob (the preview entry), so a preview needs
 * no fetch. Their content is schema-validated when the build starts (the
 * previewFixtures Vite plugin); here they are only indexed. An unknown or missing
 * screen/state resolves to an honest error, never to a guess.
 */
import type { FixtureFile } from './fixtureOps.ts';

/** screenId → stateId → fixture, in file-name order. */
export type PreviewStates<F = FixtureFile> = ReadonlyMap<string, ReadonlyMap<string, F>>;

export interface PreviewScreenListing {
  readonly screen: string;
  readonly states: readonly string[];
}

/** A preview address that names no state this build carries. */
export interface UnresolvedPreview {
  readonly kind: 'no-fixtures' | 'missing-params' | 'unknown-screen' | 'unknown-state';
  readonly screen: string | undefined;
  readonly state: string | undefined;
  readonly available: readonly PreviewScreenListing[];
}

export type PreviewResolution<F = FixtureFile> =
  | {
      readonly kind: 'ok';
      readonly screen: string;
      readonly state: string;
      readonly fixture: F;
    }
  | UnresolvedPreview;

const FILE_RE = /([^/]+)\/([^/]+)\.json$/;

/**
 * Index a glob result ({ path: parsed JSON }) by screen and state. The screen is
 * the file's parent directory, the state its base name.
 */
export function statesFromModules<F = FixtureFile>(
  modules: Readonly<Record<string, unknown>>
): PreviewStates<F> {
  const byScreen = new Map<string, Map<string, F>>();
  for (const path of Object.keys(modules).sort()) {
    const match = FILE_RE.exec(path);
    const screen = match?.[1];
    const state = match?.[2];
    if (screen === undefined || state === undefined) continue;
    const states = byScreen.get(screen) ?? new Map<string, F>();
    states.set(state, modules[path] as F);
    byScreen.set(screen, states);
  }
  return byScreen;
}

function listing<F>(states: PreviewStates<F>): PreviewScreenListing[] {
  return [...states.entries()].map(([screen, byState]) => ({
    screen,
    states: [...byState.keys()],
  }));
}

export function resolvePreviewState<F>(
  search: string,
  states: PreviewStates<F>
): PreviewResolution<F> {
  const params = new URLSearchParams(search);
  const screen = params.get('screen') ?? undefined;
  const state = params.get('state') ?? undefined;
  const available = listing(states);
  if (states.size === 0) return { kind: 'no-fixtures', screen, state, available };
  if (screen === undefined || state === undefined) {
    return { kind: 'missing-params', screen, state, available };
  }
  const byState = states.get(screen);
  if (byState === undefined) return { kind: 'unknown-screen', screen, state, available };
  const fixture = byState.get(state);
  if (fixture === undefined) return { kind: 'unknown-state', screen, state, available };
  return { kind: 'ok', screen, state, fixture };
}
