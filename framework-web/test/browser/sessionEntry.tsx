// The pages under test for session.test.ts: the real UserProvider over a probe
// that answers 401. `?preview` boots it the way a preview entry does (guards
// first, PreviewSessionGate around the app); without it, it is a plain app,
// whose 401 must still reload to sign in (the positive control).
const preview = new URLSearchParams(window.location.search).has('preview');
if (preview) await import('../../src/preview/install.ts');

const { createRoot } = await import('react-dom/client');
const { UserProvider } = await import('../../src/context/UserContext.tsx');
const { PreviewSessionGate } = await import('../../src/preview/PreviewSessionGate.tsx');

const fetchUser = (): Promise<never> =>
  Promise.reject(Object.assign(new Error('Unauthorized'), { status: 401 }));

const app = (
  <UserProvider fetchUser={fetchUser}>
    <p data-testid="app">signed in</p>
  </UserProvider>
);

const root = document.getElementById('root');
if (root === null) throw new Error('no #root');
createRoot(root).render(preview ? <PreviewSessionGate>{app}</PreviewSessionGate> : app);
