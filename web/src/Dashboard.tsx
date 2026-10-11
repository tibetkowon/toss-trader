import { useEffect, useState, type ReactNode } from 'react';
import { SettingsPage } from './SettingsPage';
import { FIELDS } from './limits';
import type { AppliedSource, SettingsStore } from './data';
import type { DashboardSource, SnapshotResult, HistoryResult } from './dashboardData';
import { chartPoints, freshness, inMarket, lossProgress, marketOf, orderLabel, ordersFor, performancePoints, type MarketFilter, type Snapshot } from './snapshot';
import { TabIcon } from './icons';

const TABS = ['홈', '거래', '종목', '성과', '시스템'] as const;
const number = (n: number | null | undefined) => n == null ? '정보 없음' : n.toLocaleString('ko-KR', { maximumFractionDigits: 2 });
const money = (n: number | null | undefined, currency = 'KRW') => n == null ? '정보 없음' : `${number(n)} ${currency === 'KRW' ? '원' : currency}`;
const pct = (n: number | null | undefined) => n == null ? '정보 없음' : `${number(n * 100)}%`;
const time = (value: string) => Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString('ko-KR') : '정보 없음';
function Card({ title, children }: { title: string; children: ReactNode }) { return <section className="card dashboard-card"><h2>{title}</h2>{children}</section>; }
function Filter({ value, change }: { value: MarketFilter; change: (v: MarketFilter) => void }) {
  return <div className="market-filter" aria-label="시장 필터">{(['ALL','KR','US'] as const).map(m => <button className={`btn small ${value === m ? 'primary' : 'ghost'}`} type="button" aria-pressed={value === m} onClick={() => change(m)} key={m}>{m === 'ALL' ? '전체' : m === 'KR' ? '국장' : '미장'}</button>)}</div>;
}
function Positions({ s, market = 'ALL' }: { s: Snapshot; market?: MarketFilter }) {
  const rows = s.positions.filter(p => inMarket(p.symbol, market));
  return <Card title="보유 포지션">{rows.length ? rows.map((p,i) => <div className="data-row" key={`${p.symbol}-${i}`}><strong>{p.symbol}</strong><span>{number(p.quantity)}주 · 평가손익 {money(p.unrealized_pnl, s.currency)}</span></div>) : <p className="muted">보유 포지션이 없습니다.</p>}</Card>;
}
function Home({ s }: { s: Snapshot }) {
  const progress = lossProgress(s);
  return <>
    <Card title="킬스위치"><strong>{s.kill_switch ? s.kill_switch.halted ? '신규 거래 중단' : '정상' : '상태 정보 없음'}</strong><p className="muted">{s.kill_switch?.reason || '중단 사유 없음'}</p></Card>
    <Card title="당일 손익"><p className="metric">{money(s.daily_pnl, s.currency)}</p><p>일일 손실 한도 사용 {pct(progress)}</p><progress aria-label="일일 손실 한도 사용률" max={1} value={Math.min(1, progress ?? 0)} /><p className="muted">시드 {money(s.seed, s.currency)} · 한도 {pct(s.daily_loss_limit_pct)}{s.currency !== 'KRW' && ` · 환율 ${money(s.fx_rate)}`}</p></Card>
    <Card title="계좌 자산·낙폭"><p className="metric">{money(s.summary?.account_equity_krw)}</p><p>현재 낙폭 {pct(s.summary?.current_drawdown)} · 최대 {pct(s.summary?.max_drawdown)}</p><p className="muted">마감 요약 기준 · 요약이 없으면 표시되지 않습니다.</p></Card>
    <Positions s={s} /><p className="muted">마지막 갱신 {time(s.updated_at)}</p>
  </>;
}
function Trading({ s }: { s: Snapshot }) {
  const [market, setMarket] = useState<MarketFilter>('ALL');
  const orders = ordersFor(s.recent_orders, market);
  return <><Filter value={market} change={setMarket} /><Positions s={s} market={market} /><Card title="주문 내역">{orders.length ? orders.map((o,i) => <div className="order-row" key={i}><div className="data-row"><strong>{o.symbol} · {orderLabel(o.status)}</strong><span>{o.side === 'BUY' || o.side === 'buy' ? '매수' : o.side === 'SELL' || o.side === 'sell' ? '매도' : o.side}</span></div><p>{number(o.quantity)}주 · {money(o.price, marketOf(o.symbol) === 'KR' ? 'KRW' : 'USD')}</p><p className="muted small">{time(o.created_at)}</p></div>) : <p className="muted">주문 내역이 없습니다.</p>}</Card></>;
}
function Symbols({ s }: { s: Snapshot }) {
  return <><Card title="활성 종목 Top 10">{s.screener.active.length ? s.screener.active.map((a,i) => <div className="order-row" key={i}><strong>{a.rank}위 · {a.symbol}</strong><p>목표가 {money(a.target, marketOf(a.symbol) === 'KR' ? 'KRW' : 'USD')}</p><p className="muted">출처: {a.origin || '정보 없음'}</p></div>) : <p className="muted">활성 종목이 없습니다.</p>}<p className="muted small">현재가가 제공되지 않아 목표가 괴리율은 표시되지 않습니다.</p></Card><Card title="탈락 종목">{s.screener.rejections.length ? s.screener.rejections.map((r,i) => <div className="data-row" key={i}><strong>{r.symbol}</strong><span>{r.reason || '사유 없음'}</span></div>) : <p className="muted">탈락 종목이 없습니다.</p>}</Card></>;
}
function Chart({ title, values, dates, percent = false }: { title: string; values: number[]; dates: string[]; percent?: boolean }) {
  return <Card title={title}><svg className="chart" viewBox="0 0 600 190" role="img" aria-label={`${title}: ${values.map((v,i) => `${dates[i]} ${percent ? pct(v) : money(v)}`).join(', ')}`}><line x1="20" x2="580" y1="160" y2="160" stroke="var(--line)" /><polyline points={chartPoints(values)} fill="none" stroke="var(--accent)" strokeWidth="3" />{values.length === 1 && <circle cx="20" cy="90" r="4" fill="var(--accent)" />}</svg><div className="data-row small"><span>{dates[0]}</span><span>{dates.at(-1)}</span></div><p className="muted">최저 {percent ? pct(Math.min(...values)) : money(Math.min(...values))} · 최고 {percent ? pct(Math.max(...values)) : money(Math.max(...values))}</p></Card>;
}
function Performance({ result, loading, retry }: { result: HistoryResult | null; loading: boolean; retry: () => void }) {
  const [market, setMarket] = useState<MarketFilter>('ALL');
  const points = performancePoints(result?.entries ?? [], market);
  const pnlPoints = points.filter(p => p.pnl !== null);
  return <><Filter value={market} change={setMarket} /><p className="muted">최근 30일 · 시장별 세션 마감 시점의 통합 계좌 자산입니다. 전체는 날짜별 마지막 자산을 사용합니다.</p>{loading && <p role="status">마감 기록을 불러오는 중입니다…</p>}{!!result?.failures && <div className="notice warn" role="status">{result.failures}개 기록을 가져오지 못했습니다. 조회된 기록만 표시합니다.</div>}<button type="button" className="btn ghost small" disabled={loading} onClick={retry}>다시 조회</button>{points.length ? <><Chart title="자산 곡선" values={points.map(p => p.equity)} dates={points.map(p => p.date)} />{pnlPoints.length > 0 && <Chart title="일별 손익" values={pnlPoints.map(p => p.pnl!)} dates={pnlPoints.map(p => p.date)} />}<Chart title="최근 30일 최대 낙폭" values={points.map(p => p.maxDrawdown)} dates={points.map(p => p.date)} percent /><Card title="일별 마감">{points.map(p => <div className="data-row" key={p.date}><span>{p.date}</span><span>{money(p.equity)} · 손익 {money(p.pnl)}</span></div>)}</Card></> : !loading && <Card title="성과 기록 없음"><p className="muted">최근 30일의 마감 기록이 없습니다.</p></Card>}</>;
}
export function Dashboard({ store, user, loadApplied, source, initialTab = 0 }: { store: SettingsStore; user: { uid: string }; loadApplied?: AppliedSource; source?: DashboardSource; initialTab?: number }) {
  const [tab, setTab] = useState(initialTab);
  const [openedSystem, setOpenedSystem] = useState(initialTab === 4);
  const [data, setData] = useState<SnapshotResult | null>(null);
  const [now, setNow] = useState(new Date());
  const [history, setHistory] = useState<HistoryResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!source) return;
    let cancelled = false, busy = false;
    async function refresh() {
      if (busy) return;
      busy = true;
      try { const next = await source!.snapshot(); if (!cancelled) setData(next); }
      catch { if (!cancelled) setData(previous => ({ snapshot: previous?.snapshot ?? null, offline: true })); }
      finally { busy = false; }
    }
    void refresh();
    const timer = window.setInterval(() => { setNow(new Date()); void refresh(); }, 30_000);
    const offline = () => setData(previous => ({ snapshot: previous?.snapshot ?? null, offline: true }));
    window.addEventListener('offline', offline);
    window.addEventListener('online', refresh);
    return () => { cancelled = true; clearInterval(timer); window.removeEventListener('online', refresh); window.removeEventListener('offline', offline); };
  }, [source]);
  useEffect(() => {
    if (tab !== 3 || !source) return;
    let cancelled = false;
    setLoading(true);
    source.history().then(result => { if (!cancelled) setHistory(result); }).catch(() => { if (!cancelled) setHistory({ entries: [], failures: 60 }); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [tab, source, attempt]);
  const s = data?.snapshot;
  const state = s ? freshness(s, now) : null;
  return <div className="dashboard">
    <div className="page-heading"><h2>{TABS[tab]}</h2>{data?.offline && <span className="badge warn">오프라인 · 마지막 스냅샷</span>}</div>
    {state === 'stale' && <div className="notice warn" role="status">장중 상태 갱신이 끊겼습니다. 마지막 갱신 {time(s!.updated_at)}</div>}
    {state === 'closed' && <p className="muted small">장외 · 갱신 대기는 정상입니다.</p>}
    <p className="muted small">시장 상태는 정규장 시간 기준입니다(휴장일·임시 거래시간 미반영).</p>
    {!source && <div className="notice warn">상태 데이터 주소가 설정되지 않았습니다.</div>}
    {!s && tab < 3 && <Card title={data?.offline ? '저장된 스냅샷이 없습니다' : source ? '상태를 불러오는 중입니다…' : '상태 데이터 없음'}><p className="muted">네트워크 연결과 상태 파일을 확인해 주세요.</p></Card>}
    {s && tab === 0 && <Home s={s} />}{s && tab === 1 && <Trading s={s} />}{s && tab === 2 && <Symbols s={s} />}
    {tab === 3 && <Performance result={history} loading={loading} retry={() => setAttempt(a => a + 1)} />}
    <div hidden={tab !== 4}>
      <Card title="실제 적용 설정"><p className="muted">트레이더 빌드 {s?.version || '정보 없음'} · 갱신 {s ? time(s.updated_at) : '정보 없음'}</p>{s && FIELDS.some(f => s.config[f.key] !== undefined) ? FIELDS.filter(f => s.config[f.key] !== undefined).map(f => <div className="data-row" key={f.key}><span>{f.label}</span><strong>{typeof s.config[f.key] === 'boolean' ? s.config[f.key] ? '사용' : '사용 안 함' : f.kind === 'percent' ? pct(s.config[f.key] as number) : `${number(s.config[f.key] as number)} ${f.unit}`}</strong></div>) : <p>적용 설정 정보가 없습니다.</p>}</Card>
      {openedSystem && <SettingsPage store={store} user={user} loadApplied={loadApplied} />}
    </div>
    <nav className="bottom-tabs" aria-label="주 메뉴">{TABS.map((label,i) => <button type="button" key={label} aria-current={tab === i ? 'page' : undefined} onClick={() => { setTab(i); if (i === 4) setOpenedSystem(true); }}><TabIcon index={i} /><span>{label}</span></button>)}</nav>
  </div>;
}
