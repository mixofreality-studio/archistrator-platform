/**
 * The honest in-frame error page for a preview that cannot be shown
 * (design-renderer-data.md §2′.1): it says what was asked for and why it cannot
 * be shown, and, for an address that names no recorded state, which
 * screen × state pairs this build does carry.
 */
import type { ReactNode } from 'react';
import type { UnresolvedPreview } from './fixtureRegistry.ts';

/** The state's fixture answers the app's session probe 401 (PreviewSessionGate). */
export interface UnauthenticatedPreview {
  readonly kind: 'unauthenticated';
}

export type PreviewFailure = UnresolvedPreview | UnauthenticatedPreview;

function reason(r: PreviewFailure): string {
  switch (r.kind) {
    case 'no-fixtures':
      return 'This preview build carries no fixture states.';
    case 'missing-params':
      return 'Name a screen and a state: ?screen=<id>&state=<id>.';
    case 'unknown-screen':
      return `This build has no screen "${r.screen ?? ''}".`;
    case 'unknown-state':
      return `Screen "${r.screen ?? ''}" has no state "${r.state ?? ''}".`;
    case 'unauthenticated':
      return (
        "This state's fixture answers the app's session probe with 401. A preview " +
        'cannot sign in, so the app has nothing to render; record the signed-in ' +
        'state, or the error the app should show.'
      );
  }
}

export interface PreviewErrorPageProps {
  readonly resolution: PreviewFailure;
  /** The page's data-testid. */
  readonly testId?: string;
}

export function PreviewErrorPage({
  resolution,
  testId = 'preview-error-page',
}: PreviewErrorPageProps): ReactNode {
  const available = resolution.kind === 'unauthenticated' ? [] : resolution.available;
  return (
    <main
      data-testid={testId}
      style={{
        maxWidth: 720,
        margin: '48px auto',
        padding: '0 24px',
        font: '14px/1.6 Inter, system-ui, sans-serif',
        color: '#1f2328',
      }}
    >
      <h1 style={{ fontSize: 20 }}>
        {resolution.kind === 'unauthenticated'
          ? 'This preview state cannot be shown'
          : 'No preview for this address'}
      </h1>
      <p>{reason(resolution)}</p>
      {available.length > 0 ? (
        <>
          <p>States in this build:</p>
          <ul>
            {available.flatMap(({ screen, states }) =>
              states.map((state) => (
                <li key={`${screen}/${state}`}>
                  <a
                    href={`?screen=${encodeURIComponent(screen)}&state=${encodeURIComponent(state)}`}
                  >
                    {screen} · {state}
                  </a>
                </li>
              ))
            )}
          </ul>
        </>
      ) : null}
    </main>
  );
}
