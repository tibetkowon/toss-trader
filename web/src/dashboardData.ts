import { historyPaths, parseSnapshot, type Snapshot, type HistoryEntry } from './snapshot';
export interface SnapshotResult { snapshot: Snapshot | null; offline: boolean }
export interface HistoryResult { entries: HistoryEntry[]; failures: number }
export interface DashboardSource {
  snapshot(): Promise<SnapshotResult>;
  history(): Promise<HistoryResult>;
}
async function read(url: string) {
  const response = await fetch(url, { cache: 'no-store', signal: AbortSignal.timeout(12_000) });
  return response;
}
export function networkSource(url: string): DashboardSource {
  const key = `trader:snapshot:${url}`;
  let last: Snapshot | null = null;
  return {
    async snapshot() {
      try {
        const response = await read(url);
        if (!response.ok) throw new Error('상태 조회 실패');
        last = parseSnapshot(await response.json());
        try { localStorage.setItem(key, JSON.stringify(last)); } catch { /* 저장 공간이 없어도 온라인 조회는 유지합니다. */ }
        return { snapshot: last, offline: false };
      } catch {
        if (!last) {
          try { last = parseSnapshot(JSON.parse(localStorage.getItem(key) ?? 'null')); } catch { /* 최초 접속 또는 손상된 캐시 */ }
        }
        return { snapshot: last, offline: true };
      }
    },
    async history() {
      const entries: HistoryEntry[] = [];
      let failures = 0;
      const paths = historyPaths(new Date());
      // 한 번에 6개씩 조회해 느린 연결에서도 요청 수를 제한합니다.
      for (let i = 0; i < paths.length; i += 6) {
        await Promise.all(paths.slice(i,i + 6).map(async item => {
          try {
            const response = await read(new URL(item.path, new URL(url, window.location.href)).href);
            if (response.status === 404) return;
            if (!response.ok) throw new Error('성과 조회 실패');
            const snapshot = parseSnapshot(await response.json());
            if (!snapshot.summary) return; // 요약 도입 전의 옛 기록은 실패가 아니라 건너뜁니다
            entries.push({ date: item.date, market: item.market, snapshot });
          } catch { failures++; }
        }));
      }
      return { entries, failures };
    },
  };
}
