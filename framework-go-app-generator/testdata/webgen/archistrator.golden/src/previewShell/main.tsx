/**
 * PREVIEW shell entry (design-renderer-data.md §2′.1): the SAME <App/> the
 * browser SPA boots (main.tsx), in preview mode with fixture data. It differs from
 * main.tsx in exactly three seams:
 *
 *   - the transport: fixtureOpsClient over the chosen state's fixture, marked
 *     `transport: 'fixture'`, instead of restOpsClient;
 *   - the history: a memory history seeded at the fixture's route, so a preview
 *     never reads or writes the page URL, instead of browser history;
 *   - the network and navigation: framework-web's preview guards (the FIRST
 *     import below) make every other request throw and refuse any second tab or
 *     window (a nested preview), and PreviewAlarm shows every miss and refusal;
 *   - the session: a 401 on the app's session probe cannot sign in here, so
 *     PreviewSessionGate shows the preview error page instead of reloading.
 *
 * `?screen=<id>&state=<id>` picks the fixture; an unknown or missing pair renders
 * an honest error page, never a guess. The QueryClient never retries, so an
 * error state shows at once.
 *
 * Built by vite.preview.config.ts into dist-preview/, never dist/.
 * Scaffolded by framework-go-app-generator/webgen; the app's own code from here on.
 */
// FIRST: nothing may evaluate before the guards are in place.
import '@mixofreality-studio/archistrator-platform-framework-web/preview/install';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory } from '@tanstack/react-router';
import {
  PreviewAlarm,
  PreviewErrorPage,
  PreviewSessionGate,
  raisePreviewIncident,
  resolvePreviewState,
  statesFromModules,
} from '@mixofreality-studio/archistrator-platform-framework-web/preview';
import '../index.css';
import App from '../App';
import { fixtureOpsClient, type FixtureFile } from '../api/fixtureOps';
import { OpsClientProvider } from '../api/opsContext';
import { createAppRouter } from '../routes/router';

const modules = import.meta.glob<unknown>('@preview-fixtures/web-client/*/*.json', {
  eager: true,
  import: 'default',
});
const resolution = resolvePreviewState(
  window.location.search,
  statesFromModules<FixtureFile>(modules)
);

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('Root element not found');
}
const root = createRoot(rootElement);

if (resolution.kind !== 'ok') {
  document.title = 'archistrator · preview · no such state';
  root.render(<PreviewErrorPage resolution={resolution} />);
} else {
  const { screen, state, fixture } = resolution;
  document.title = `Preview · ${screen} · ${state} · fixture data`;

  const ops = fixtureOpsClient(fixture.ops, {
    onMiss: (opId) => {
      raisePreviewIncident({ kind: 'fixture-miss', detail: opId });
    },
  });
  const router = createAppRouter(createMemoryHistory({ initialEntries: [fixture.route] }), {
    ops,
  });
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 1000 * 30, retry: false, refetchOnWindowFocus: false },
    },
  });

  root.render(
    <StrictMode>
      <PreviewSessionGate>
        <QueryClientProvider client={queryClient}>
          <OpsClientProvider value={{ ops, transport: 'fixture' }}>
            <App router={router} />
          </OpsClientProvider>
        </QueryClientProvider>
      </PreviewSessionGate>
      <PreviewAlarm />
    </StrictMode>
  );
}
