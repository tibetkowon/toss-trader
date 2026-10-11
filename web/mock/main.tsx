import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '../src/styles.css';
import { Frame } from '../src/Frame';
import { Login } from '../src/Login';
import { Dashboard } from '../src/Dashboard';
import { historyPaths, parseSnapshot } from '../src/snapshot';
import type { DashboardSource } from '../src/dashboardData';
import { createMockStore, mockApplied, mockSnapshot } from './mockStore';

// 예: /mock/index.html?state=pending  또는  ?view=login  또는  ?state=denied
const params = new URLSearchParams(window.location.search);
const state = params.get('state') ?? 'settings';
const view = params.get('view') ?? 'home';

const store = createMockStore(state);
const applied = mockApplied(state);

const source: DashboardSource = {
  snapshot: async () => ({ snapshot: parseSnapshot(mockSnapshot(state)), offline: state === 'offline' }),
  history: async () => ({ failures: state === 'offline' ? 2 : 0, entries: state === 'empty' ? [] : historyPaths(new Date()).map((item, i) => {
    const value = mockSnapshot(state);
    const equity = 100000 + Math.sin(i / 4) * 2500 - i * 35;
    value.updated_at = `${item.date}T${item.market === 'KR' ? '06' : '21'}:00:00Z`;
    value.summary = { account_equity_krw: equity, day_pnl_krw: Math.cos(i / 4) * 300, current_drawdown: 0.02, max_drawdown: 0.04 };
    return { date: item.date, market: item.market, snapshot: parseSnapshot(value) };
  }) }),
};
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
      <Dashboard store={store} user={{ uid: 'mock-admin' }} loadApplied={applied} source={source} initialTab={Math.max(0, ['home', 'trading', 'symbols', 'performance', 'settings'].indexOf(view))} />
    </Frame>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Preview />
  </StrictMode>,
);
