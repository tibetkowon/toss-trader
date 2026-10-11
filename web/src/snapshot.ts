import { FIELDS, type Settings } from './limits';

export type Market = 'KR' | 'US';
export type MarketFilter = Market | 'ALL';
export interface Position { symbol: string; quantity: number; unrealized_pnl: number }
export interface Order { symbol: string; side: string; quantity: number; price: number; status: string; created_at: string }
export interface Summary { account_equity_krw: number; day_pnl_krw: number | null; current_drawdown: number | null; max_drawdown: number | null }
export interface Snapshot {
  updated_at: string; currency: string; fx_rate: number | null; seed: number | null; daily_pnl: number | null;
  daily_loss_limit_pct: number | null; kill_switch: { halted: boolean; reason: string } | null;
  positions: Position[]; recent_orders: Order[];
  screener: { active: { symbol: string; rank: number; target: number; origin: string }[]; rejections: { symbol: string; reason: string }[] };
  summary: Summary | null; config: Partial<Settings>; version: string; config_version: number | null;
}
const obj = (v: unknown): Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {};
const num = (v: unknown): number | null => typeof v === 'number' && Number.isFinite(v) ? v : null;
const str = (v: unknown): string => typeof v === 'string' ? v : '';
const rows = (v: unknown) => Array.isArray(v) ? v.map(obj).filter(r => str(r.symbol)) : [];
export function parseSnapshot(value: unknown): Snapshot {
  const s = obj(value);
  if (!str(s.updated_at) || !Number.isFinite(Date.parse(str(s.updated_at)))) throw new Error('갱신 시각이 없는 상태 파일');
  const c = obj(s.config), summary = obj(s.summary), screen = obj(s.screener), kill = obj(s.kill_switch);
  const config = Object.fromEntries(FIELDS.filter(f => f.kind === 'bool' ? typeof c[f.key] === 'boolean' : num(c[f.key]) !== null).map(f => [f.key, c[f.key]])) as Partial<Settings>;
  return {
    updated_at: str(s.updated_at), currency: str(s.currency) || 'KRW', fx_rate: num(s.fx_rate), seed: num(s.seed), daily_pnl: num(s.daily_pnl),
    daily_loss_limit_pct: num(s.daily_loss_limit_pct) ?? num(c.daily_loss_limit_pct),
    kill_switch: typeof kill.halted === 'boolean' ? { halted: kill.halted, reason: str(kill.reason) } : null,
    positions: rows(s.positions).filter(r => num(r.quantity) !== null && num(r.unrealized_pnl) !== null).map(r => ({ symbol: str(r.symbol), quantity: r.quantity as number, unrealized_pnl: r.unrealized_pnl as number })),
    recent_orders: rows(s.recent_orders).filter(r => num(r.quantity) !== null && num(r.price) !== null).map(r => ({ symbol: str(r.symbol), side: str(r.side), quantity: r.quantity as number, price: r.price as number, status: str(r.status), created_at: str(r.created_at) })),
    screener: {
      active: rows(screen.active).filter(r => num(r.target) !== null).map(r => ({ symbol: str(r.symbol), rank: num(r.rank) ?? 999, target: r.target as number, origin: str(r.origin) })).sort((a,b) => a.rank - b.rank).slice(0,10),
      rejections: rows(screen.rejections).map(r => ({ symbol: str(r.symbol), reason: str(r.reason) })),
    },
    summary: num(summary.account_equity_krw) === null ? null : { account_equity_krw: summary.account_equity_krw as number, day_pnl_krw: num(summary.day_pnl_krw), current_drawdown: num(summary.current_drawdown), max_drawdown: num(summary.max_drawdown) },
    config, version: str(s.version), config_version: num(s.config_version),
  };
}
export const marketOf = (symbol: string): Market => /^\d{6}$/.test(symbol) ? 'KR' : 'US';
export const inMarket = (symbol: string, market: MarketFilter) => market === 'ALL' || marketOf(symbol) === market;
export const ordersFor = (orders: Order[], market: MarketFilter) => orders.filter(o => o.status !== 'SkippedZeroShares' && inMarket(o.symbol, market)).sort((a,b) => (Date.parse(b.created_at) || 0) - (Date.parse(a.created_at) || 0));
export const orderLabel = (status: string) => ({ Bought: '돌파 진입', StoppedOut: '손절', ClosedEndOfDay: '강제청산' }[status] ?? status);
export function lossProgress(s: Snapshot): number | null {
  if (s.daily_pnl === null || !s.seed || s.seed <= 0 || !s.daily_loss_limit_pct || s.daily_loss_limit_pct <= 0) return null;
  return Math.max(0, -s.daily_pnl / (s.seed * s.daily_loss_limit_pct));
}
function localClock(now: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, weekday: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(now);
  const part = (key: string) => parts.find(p => p.type === key)?.value ?? '';
  return { weekday: !['Sat','Sun'].includes(part('weekday')), minutes: Number(part('hour')) * 60 + Number(part('minute')) };
}
// shortcut: 휴장일 정보가 스냅샷에 없어 정규장 시간 기준입니다. 거래일 API가 제공되면 휴장일도 반영합니다.
export function marketOpen(now: Date): boolean {
  const kr = localClock(now, 'Asia/Seoul'), us = localClock(now, 'America/New_York');
  return (kr.weekday && kr.minutes >= 540 && kr.minutes < 930) || (us.weekday && us.minutes >= 570 && us.minutes < 960);
}
export function freshness(s: Snapshot, now: Date): 'closed' | 'stale' | 'fresh' {
  if (!marketOpen(now)) return 'closed';
  return now.getTime() - Date.parse(s.updated_at) > Math.max(180, (s.config.heartbeat_interval_seconds ?? 60) * 3) * 1000 ? 'stale' : 'fresh';
}
export interface HistoryEntry { date: string; market: Market; snapshot: Snapshot }
export function historyPaths(now: Date): { date: string; market: Market; path: string }[] {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Seoul', year: 'numeric', month: '2-digit', day: '2-digit' }).format(now);
  return Array.from({ length: 30 }, (_, i) => {
    const date = new Date(`${today}T00:00:00Z`); date.setUTCDate(date.getUTCDate() - i);
    const day = date.toISOString().slice(0,10);
    return (['KR','US'] as const).map(market => ({ date: day, market, path: `history/${day}-${market}.json` }));
  }).flat();
}
export function performancePoints(entries: HistoryEntry[], market: MarketFilter) {
  const sorted = entries.filter(e => e.snapshot.summary && (market === 'ALL' || e.market === market)).sort((a,b) => a.date.localeCompare(b.date) || Date.parse(a.snapshot.updated_at) - Date.parse(b.snapshot.updated_at));
  const days = new Map<string, { date: string; equity: number; pnl: number | null }>();
  for (const e of sorted) {
    const summary = e.snapshot.summary!;
    const prior = days.get(e.date);
    days.set(e.date, { date: e.date, equity: summary.account_equity_krw, pnl: prior ? (prior.pnl !== null && summary.day_pnl_krw !== null ? prior.pnl + summary.day_pnl_krw : null) : summary.day_pnl_krw });
  }
  let peak = 0, maxDrawdown = 0, previous: number | null = null;
  return [...days.values()].map(p => {
    peak = Math.max(peak, p.equity);
    const drawdown = peak > 0 ? Math.max(0, (peak - p.equity) / peak) : 0;
    maxDrawdown = Math.max(maxDrawdown, drawdown);
    const pnl = p.pnl ?? (previous === null ? null : p.equity - previous);
    previous = p.equity;
    return { ...p, pnl, drawdown, maxDrawdown };
  });
}
export function chartPoints(values: number[]): string {
  const min = Math.min(...values), max = Math.max(...values);
  return values.map((value,i) => `${20 + i * 560 / Math.max(1, values.length - 1)},${max === min ? 90 : 160 - (value - min) / (max - min) * 140}`).join(' ');
}
