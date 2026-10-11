import { afterEach, expect, it, vi } from 'vitest';
import { networkSource } from './dashboardData';
const body = { updated_at: '2026-10-09T00:00:00Z', daily_pnl: -100, summary: { account_equity_krw: 10000 } };
afterEach(() => vi.unstubAllGlobals());
it('네트워크 우선 조회 후 실패 시 주소별 마지막 스냅샷을 복구합니다', async () => {
  const cache = new Map<string,string>();
  vi.stubGlobal('localStorage', { getItem: (key: string) => cache.get(key), setItem: (key: string, value: string) => cache.set(key,value) });
  const fetcher = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(body))).mockRejectedValue(new Error('offline'));
  vi.stubGlobal('fetch', fetcher);
  expect((await networkSource('https://example.com/status.json').snapshot()).offline).toBe(false);
  expect(fetcher.mock.calls[0][1].cache).toBe('no-store');
  const offline = await networkSource('https://example.com/status.json').snapshot();
  expect(offline.offline).toBe(true); expect(offline.snapshot?.daily_pnl).toBe(-100);
  expect((await networkSource('https://other.example/status.json').snapshot()).snapshot).toBeNull();
});
it('손상 캐시와 저장 공간 오류를 처리합니다', async () => {
  vi.stubGlobal('localStorage', { getItem: () => 'broken', setItem: () => { throw new Error(); } });
  vi.stubGlobal('fetch', vi.fn().mockRejectedValueOnce(new Error()).mockResolvedValueOnce(new Response(JSON.stringify(body))).mockRejectedValue(new Error()));
  const source = networkSource('/status.json');
  expect((await source.snapshot()).snapshot).toBeNull(); expect((await source.snapshot()).offline).toBe(false); expect((await source.snapshot()).snapshot?.daily_pnl).toBe(-100);
});
it('60개 기록을 시도하며 404만 무시합니다', async () => {
  vi.stubGlobal('window', { location: { href: 'https://example.com/app/' } });
  const fetcher = vi.fn().mockResolvedValue(new Response('', { status: 404 })).mockResolvedValueOnce(new Response(JSON.stringify(body))).mockResolvedValueOnce(new Response('', { status: 500 }));
  vi.stubGlobal('fetch', fetcher);
  const result = await networkSource('https://example.com/data/status.json').history();
  expect(fetcher).toHaveBeenCalledTimes(60);
  expect(fetcher.mock.calls[0][0]).toMatch(/^https:\/\/example.com\/data\/history\/\d{4}-\d{2}-\d{2}-KR.json$/);
  expect(result.entries).toHaveLength(1); expect(result.failures).toBe(1);
});
