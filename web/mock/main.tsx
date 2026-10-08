import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '../src/styles.css';
import { Frame } from '../src/Frame';
import { Login } from '../src/Login';
import { SettingsPage } from '../src/SettingsPage';
import { createMockStore, mockApplied } from './mockStore';

// 예: /mock/index.html?state=pending  또는  ?view=login  또는  ?state=denied
const params = new URLSearchParams(window.location.search);
const state = params.get('state') ?? 'settings';
const view = params.get('view') ?? 'settings';

const store = createMockStore(state);
const applied = mockApplied(state);

function Preview() {
  if (view === 'login') {
    return (
      <Frame>
        <Login
          onSignIn={async () => {
            await new Promise((resolve) => setTimeout(resolve, 600));
            throw new Error('mock');
          }}
        />
      </Frame>
    );
  }
  return (
    <Frame email="admin@example.com" onLogout={() => {}}>
      <SettingsPage store={store} user={{ uid: 'mock-admin' }} loadApplied={applied} />
    </Frame>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Preview />
  </StrictMode>,
);
