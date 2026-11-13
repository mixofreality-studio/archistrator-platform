import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import createClient from 'openapi-fetch';
import { OpsClientProvider } from '../api/opsContext';
import { restOpsClient } from '../api/ops.gen';
import type { paths } from '../contracts/schema';
import { App } from './routes/App';

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('Root element not found');
}
const ops = restOpsClient(createClient<paths>({ baseUrl: '' }));
createRoot(rootElement).render(
  <StrictMode>
    <OpsClientProvider value={{ ops, transport: 'rest' }}>
      <App />
    </OpsClientProvider>
  </StrictMode>
);
