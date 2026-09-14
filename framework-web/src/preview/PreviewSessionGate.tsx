/**
 * Shows the preview error page in place of the app once the app's session probe
 * has answered 401 (an `unauthenticated` incident, raised by the preview guards
 * when they claim the announced 401). A real app would reload to sign in; a
 * preview cannot sign in, and a reload would answer the same fixture 401
 * forever. The preview entry wraps the app in it.
 */
import { useSyncExternalStore, type ReactNode } from 'react';
import { PreviewErrorPage } from './PreviewErrorPage.tsx';
import { previewIncidents, subscribePreviewIncidents } from './previewIncidents.ts';

export interface PreviewSessionGateProps {
  readonly children: ReactNode;
  /** The error page's data-testid. */
  readonly testId?: string;
}

export function PreviewSessionGate({ children, testId }: PreviewSessionGateProps): ReactNode {
  const incidents = useSyncExternalStore(subscribePreviewIncidents, previewIncidents);
  if (!incidents.some((i) => i.kind === 'unauthenticated')) return children;
  return (
    <PreviewErrorPage
      resolution={{ kind: 'unauthenticated' }}
      {...(testId === undefined ? {} : { testId })}
    />
  );
}
