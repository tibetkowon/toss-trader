import { expect, it } from 'vitest';
import { chartPoints, freshness, historyPaths, lossProgress, marketOf, marketOpen, orderLabel, ordersFor, parseSnapshot, performancePoints } from './snapshot';
const base = { updated_at: '2026-10-09T00:00:00Z', daily_pnl: -600, seed: 10000, daily_loss_limit_pct: 0.05 };
const snapshot = (equity: number, pnl?: number) => parseSnapshot({ ...base, summary: { account_equity_krw: equity, day_pnl_krw: pnl } });
it('잘못된 파일과 숫자를 검증하고 설정은 허용 필드만 읽습니다', () => {
  expect(() => parseSnapshot({})).toThrow();
  expect(() => parseSnapshot({ updated_at: 'invalid' })).toThrow();
  const s = parseSnapshot({ ...base, positions: null, daily_pnl: '100', summary: { account_equity_krw: Infinity }, config: { secret: 'hidden', k: 0.5 } });
  expect(s.positions).toEqual([]); expect(s.daily_pnl).toBeNull(); expect(s.summary).toBeNull(); expect(s.config).toEqual({ k: 0.5 });
});
it('활성 종목을 순위순 Top10으로 제한합니다', () => {
  const s = parseSnapshot({ ...base, screener: { active: Array.from({ length: 12 }, (_,i) => ({ symbol: `A${i}`, rank: 12-i, target: 10 })) } });
  expect(s.screener.active).toHaveLength(10); expect(s.screener.active[0].rank).toBe(1);
});
it('손실 한도 초과, 수익, 유효하지 않은 분모를 처리합니다', () => {
  expect(lossProgress(parseSnapshot(base))).toBe(1.2);
  expect(lossProgress(parseSnapshot({ ...base, daily_pnl: 100 }))).toBe(0);
  expect(lossProgress(parseSnapshot({ ...base, seed: 0 }))).toBeNull();
});
it('국장 6자리와 주문 제외 및 실제 상태를 변환합니다', () => {
  expect(marketOf('005930')).toBe('KR'); expect(marketOf('12345')).toBe('US');
  const orders = ['Bought','StoppedOut','ClosedEndOfDay','SkippedZeroShares'].map(status => ({ symbol: '005930', status, side: 'BUY', quantity: 1, price: 100, created_at: base.updated_at }));
  expect(ordersFor(orders, 'KR')).toHaveLength(3); expect(ordersFor(orders, 'US')).toEqual([]);
  expect(orders.slice(0,3).map(o => orderLabel(o.status))).toEqual(['돌파 진입','손절','강제청산']);
});
it('국장, 미국 서머타임과 표준시, 주말을 구분합니다', () => {
  expect(marketOpen(new Date('2026-10-09T00:00:00Z'))).toBe(true);
  expect(marketOpen(new Date('2026-10-09T06:30:00Z'))).toBe(false);
  expect(marketOpen(new Date('2026-07-06T13:30:00Z'))).toBe(true);
  expect(marketOpen(new Date('2026-01-05T13:30:00Z'))).toBe(false);
  expect(marketOpen(new Date('2026-01-05T14:30:00Z'))).toBe(true);
  expect(marketOpen(new Date('2026-10-10T12:00:00Z'))).toBe(false);
});
it('장중 끊김만 경고하고 하트비트를 반영합니다', () => {
  expect(freshness(parseSnapshot(base), new Date('2026-10-09T00:04:00Z'))).toBe('stale');
  expect(freshness(parseSnapshot({ ...base, config: { heartbeat_interval_seconds: 600 } }), new Date('2026-10-09T00:04:00Z'))).toBe('fresh');
  expect(freshness(parseSnapshot(base), new Date('2026-10-10T12:00:00Z'))).toBe('closed');
});
it('한국 날짜 기준 최근 30일과 두 시장의 경로를 만듭니다', () => {
  const paths = historyPaths(new Date('2026-01-01T16:00:00Z'));
  expect(paths).toHaveLength(60); expect(paths[0].path).toBe('history/2026-01-02-KR.json'); expect(paths.at(-1)?.path).toBe('history/2025-12-04-US.json');
});
it('통합 자산 중복 합산을 방지하고 손익 및 최대 낙폭을 계산합니다', () => {
  const entries = [
    { date: '2026-10-08', market: 'US' as const, snapshot: { ...snapshot(110, 5), updated_at: '2026-10-08T21:00:00Z' } },
    { date: '2026-10-08', market: 'KR' as const, snapshot: { ...snapshot(105, 5), updated_at: '2026-10-08T06:00:00Z' } },
    { date: '2026-10-09', market: 'KR' as const, snapshot: snapshot(99) },
  ];
  const points = performancePoints(entries, 'ALL');
  expect(points.map(p => p.equity)).toEqual([110,99]); expect(points.map(p => p.pnl)).toEqual([10,-11]); expect(points[1].maxDrawdown).toBeCloseTo(0.1);
  expect(performancePoints(entries, 'US')).toHaveLength(1); expect(performancePoints([], 'ALL')).toEqual([]);
});
it('단일 값, 동일 값, 음수를 SVG 좌표로 변환합니다', () => {
  expect(chartPoints([0])).toBe('20,90'); expect(chartPoints([5,5])).toBe('20,90 580,90'); expect(chartPoints([-10,10])).toBe('20,160 580,20');
});
