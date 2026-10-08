import {
  collection,
  doc,
  onSnapshot,
  runTransaction,
  serverTimestamp,
  type Firestore,
  type Unsubscribe,
} from 'firebase/firestore';
import type { Settings } from './limits';

export type StoredSettings = Settings & { version: number; updated_by?: string };

export class ConflictError extends Error {
  constructor(readonly current: number) {
    super(`설정이 버전 ${current}로 먼저 저장되었습니다`);
  }
}

// 설정 문서를 실시간으로 구독합니다. 권한이 없으면 onError로 알려 줍니다.
export function watchSettings(
  db: Firestore,
  onData: (stored: StoredSettings | null) => void,
  onError: (err: { code?: string }) => void,
): Unsubscribe {
  return onSnapshot(
    doc(db, 'settings', 'trader'),
    (snap) => onData(snap.exists() ? (snap.data() as StoredSettings) : null),
    onError,
  );
}

// 현재 버전을 확인한 뒤 설정과 변경 이력을 한 번에 씁니다. 다른 곳에서 먼저 저장됐으면 ConflictError입니다.
export async function saveSettings(
  db: Firestore,
  uid: string,
  expectedVersion: number,
  next: Settings,
): Promise<number> {
  const ref = doc(db, 'settings', 'trader');
  const history = doc(collection(db, 'settings_history'));
  return runTransaction(db, async (tx) => {
    const snap = await tx.get(ref);
    const current = snap.exists() ? Number(snap.data().version) : 0;
    if (current !== expectedVersion) {
      throw new ConflictError(current);
    }
    const version = current + 1;
    const payload = { ...next, version, updated_at: serverTimestamp(), updated_by: uid };
    tx.set(ref, payload);
    tx.set(history, payload);
    return version;
  });
}

export interface AppliedConfig {
  version: number | null; // null은 상태 파일에 설정 버전이 없는 경우(이전 버전 트레이더)
  updatedAt: string | null;
}

// 트레이더가 올린 상태 파일에서 지금 실행 중인 설정 버전을 읽습니다. 캐시를 쓰지 않습니다.
export async function fetchAppliedConfig(url: string): Promise<AppliedConfig> {
  const res = await fetch(url, { cache: 'no-store' });
  if (!res.ok) {
    throw new Error(`상태 파일 응답 ${res.status}`);
  }
  const body = (await res.json()) as { config_version?: number; updated_at?: string };
  return {
    version: typeof body.config_version === 'number' ? body.config_version : null,
    updatedAt: body.updated_at ?? null,
  };
}
