// 미리보기 전용 목업 저장소입니다. Firebase 없이 화면 상태를 바꿔 볼 수 있게 합니다.
// 실제 앱 빌드에는 포함되지 않습니다(dev 서버에서만 /mock/index.html로 엽니다).
import { ConflictError, type AppliedConfig, type SettingsStore, type StoredSettings } from '../src/data';
import { DEFAULTS, type Settings } from '../src/limits';

const SAMPLE: Settings = { ...DEFAULTS, stop_loss_pct: 0.015, active_count: 8, rank_depth: 40 };
const NOOP = () => {};

// state 값: settings(기본), pending(저장 v5, 실행 v4), empty(문서 없음), denied(권한 없음)
export function createMockStore(state: string): SettingsStore {
  if (state === 'denied') {
    return {
      watch(_onData, onError) {
        onError({ code: 'permission-denied' });
        return NOOP;
      },
      save: async () => {
        throw new Error('denied');
      },
    };
  }

  let current: StoredSettings | null =
    state === 'empty' ? null : { ...SAMPLE, version: state === 'pending' ? 5 : 4, updated_by: 'mock-admin' };
  const listeners = new Set<(stored: StoredSettings | null) => void>();

  return {
    watch(onData) {
      // 첫 값은 바로 보내고, 이후 저장될 때마다 다시 보냅니다.
      setTimeout(() => onData(current), 300);
      listeners.add(onData);
      return () => {
        listeners.delete(onData);
      };
    },
    async save(_uid, expectedVersion, next) {
      await new Promise((resolve) => setTimeout(resolve, 700));
      const existing = current?.version ?? 0;
      if (existing !== expectedVersion) {
        throw new ConflictError(existing);
      }
      current = { ...next, version: existing + 1, updated_by: 'mock-admin' };
      listeners.forEach((listener) => listener(current));
      return existing + 1;
    },
  };
}

export function mockApplied(state: string): () => Promise<AppliedConfig> {
  return async () => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    if (state === 'offline') throw new Error('offline');
    if (state === 'legacy') return { version: null, updatedAt: null };
    if (state === 'pending') return { version: 4, updatedAt: '2026-10-08T15:20:00+09:00' };
    return { version: 4, updatedAt: '2026-10-08T15:20:00+09:00' };
  };
}

export function mockSnapshot(state: string) {
  return {
    updated_at: new Date(Date.now() - (state === 'stale' ? 3_600_000 : 0)).toISOString(),
    daily_pnl: -1250, seed: 100000, currency: 'KRW', fx_rate: 1380, daily_loss_limit_pct: 0.05,
    kill_switch: { halted: state === 'halted', reason: state === 'halted' ? '일일 손실 한도 도달' : '' },
    positions: state === 'empty' ? [] : [{ symbol: '005930', quantity: 1, unrealized_pnl: -1250 }],
    recent_orders: state === 'empty' ? [] : [
      { symbol: '005930', side: 'BUY', quantity: 1, price: 71000, status: 'Bought', created_at: new Date().toISOString() },
      { symbol: 'AAPL', side: 'SELL', quantity: 0.3, price: 220, status: 'StoppedOut', created_at: new Date().toISOString() },
      { symbol: 'TSLA', side: 'SELL', quantity: 0.2, price: 250, status: 'ClosedEndOfDay', created_at: new Date().toISOString() },
      { symbol: '000660', side: 'BUY', quantity: 0, price: 200000, status: 'SkippedZeroShares', created_at: new Date().toISOString() },
    ],
    screener: {
      active: state === 'empty' ? [] : [{ symbol: '005930', rank: 1, target: 71000, origin: 'ranking' }, { symbol: 'AAPL', rank: 2, target: 225, origin: 'watchlist' }],
      rejections: state === 'empty' ? [] : [{ symbol: '000660', reason: '시드로 1주 매수 불가' }, { symbol: 'TSLA', reason: '노이즈 상한 초과' }],
    },
    summary: { account_equity_krw: 98750, day_pnl_krw: -1250, current_drawdown: 0.025, max_drawdown: 0.035 },
    config: SAMPLE, config_version: 4, version: 'mock-a1b2c3d',
  };
}
