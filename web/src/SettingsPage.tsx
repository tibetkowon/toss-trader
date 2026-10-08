import { useEffect, useMemo, useState } from 'react';
import type { User } from 'firebase/auth';
import type { Firestore } from 'firebase/firestore';
import {
  DEFAULTS,
  FIELDS,
  GROUP_LABELS,
  READ_ONLY_RULES,
  crossErrors,
  diffSettings,
  fieldDef,
  isStrategyKey,
  parseDraft,
  rangeText,
  toDraft,
  type Change,
  type Draft,
  type Group,
  type Key,
  type Settings,
} from './limits';
import { ConflictError, fetchAppliedConfig, saveSettings, watchSettings, type AppliedConfig, type StoredSettings } from './data';

interface Props {
  db: Firestore;
  user: User;
  snapshotUrl?: string;
}

const GROUPS: Group[] = ['risk', 'strategy', 'screener', 'ops'];

function sameDraft(a: Draft, b: Draft): boolean {
  return FIELDS.every((f) => a[f.key] === b[f.key]);
}

export function SettingsPage({ db, user, snapshotUrl }: Props) {
  const [stored, setStored] = useState<StoredSettings | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [denied, setDenied] = useState(false);
  const [base, setBase] = useState<StoredSettings | null>(null);
  const [draft, setDraft] = useState<Draft>(toDraft(DEFAULTS));
  const [pending, setPending] = useState<{ values: Settings; changes: Change[] } | null>(null);
  const [message, setMessage] = useState<{ kind: 'ok' | 'error'; text: string } | null>(null);
  const [saving, setSaving] = useState(false);
  const [applied, setApplied] = useState<AppliedConfig | null>(null);
  const [appliedError, setAppliedError] = useState(false);

  useEffect(() => {
    return watchSettings(
      db,
      (next) => {
        setLoaded(true);
        setStored(next);
      },
      (err) => {
        if (err.code === 'permission-denied') {
          setDenied(true);
        } else {
          setMessage({ kind: 'error', text: '설정을 불러오지 못했습니다. 잠시 후 다시 시도하세요.' });
        }
      },
    );
  }, [db]);

  // 문서가 없으면 기본값을 버전 0으로 보고, 첫 저장이 버전 1을 만듭니다.
  const source: StoredSettings = stored ?? { ...DEFAULTS, version: 0 };

  // 편집 중이 아니면 최신 값으로 맞추고, 편집 중이면 기준 버전만 유지합니다.
  useEffect(() => {
    if (!loaded) return;
    if (base === null) {
      setBase(source);
      setDraft(toDraft(source));
      return;
    }
    if (base.version === source.version) return;
    if (sameDraft(draft, toDraft(base))) {
      setBase(source);
      setDraft(toDraft(source));
    }
  }, [loaded, source.version, base, draft]);

  useEffect(() => {
    if (!snapshotUrl) return;
    let cancelled = false;
    const load = () =>
      fetchAppliedConfig(snapshotUrl)
        .then((value) => {
          if (!cancelled) {
            setApplied(value);
            setAppliedError(false);
          }
        })
        .catch(() => {
          if (!cancelled) setAppliedError(true);
        });
    load();
    const timer = window.setInterval(load, 60_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [snapshotUrl]);

  const parsed = useMemo(() => parseDraft(draft), [draft]);
  const problems = useMemo(
    () => (Object.keys(parsed.errors).length > 0 ? [] : crossErrors(parsed.values)),
    [parsed],
  );
  const dirty = base !== null && !sameDraft(draft, toDraft(base));
  const outdated = base !== null && source.version !== base.version;
  const canSave = dirty && !outdated && !saving && Object.keys(parsed.errors).length === 0 && problems.length === 0;

  const update = (key: Key, text: string) => {
    setMessage(null);
    setDraft((prev) => ({ ...prev, [key]: text }));
  };

  const askSave = () => {
    setMessage(null);
    setPending({ values: parsed.values, changes: base ? diffSettings(baseSettings(base), parsed.values) : [] });
  };

  const confirmSave = async () => {
    if (!pending || !base) return;
    setSaving(true);
    try {
      const version = await saveSettings(db, user.uid, base.version, pending.values);
      setPending(null);
      setMessage({ kind: 'ok', text: `버전 ${version}로 저장했습니다. 다음 세션부터 적용됩니다.` });
    } catch (err) {
      setPending(null);
      if (err instanceof ConflictError) {
        setMessage({ kind: 'error', text: `다른 곳에서 버전 ${err.current}이 먼저 저장되었습니다. 다시 불러온 뒤 편집하세요.` });
      } else {
        setMessage({ kind: 'error', text: '저장에 실패했습니다. 연결 상태를 확인한 뒤 다시 시도하세요.' });
      }
    } finally {
      setSaving(false);
    }
  };

  const reload = () => {
    setBase(source);
    setDraft(toDraft(source));
    setMessage(null);
  };

  if (denied) {
    return (
      <section className="card">
        <h2>접근 권한이 없습니다</h2>
        <p>이 계정은 설정을 볼 수 없습니다. 아래 UID를 관리자 목록(admins 컬렉션)에 문서 ID로 등록하세요.</p>
        <code className="uid">{user.uid}</code>
      </section>
    );
  }

  if (!loaded) {
    return <p className="muted">설정을 불러오는 중입니다…</p>;
  }

  return (
    <>
      <section className="card status">
        <div>
          <span className="label">저장된 버전</span>
          <strong>{stored ? `v${stored.version}` : '없음'}</strong>
        </div>
        <div>
          <span className="label">실행 중인 버전</span>
          <strong>
            {!snapshotUrl
              ? '확인 안 함'
              : appliedError
                ? '확인 실패'
                : applied?.version == null
                  ? applied
                    ? '알 수 없음'
                    : '확인 중'
                  : `v${applied.version}`}
          </strong>
        </div>
        {stored && applied?.version != null && stored.version > applied.version && (
          <p className="notice">다음 세션부터 적용 대기 중입니다.</p>
        )}
        {outdated && (
          <p className="notice warn">
            다른 곳에서 새 버전이 저장되었습니다. 편집 내용을 버리고 다시 불러오세요.
            <button type="button" className="ghost" onClick={reload}>다시 불러오기</button>
          </p>
        )}
      </section>

      {GROUPS.map((group) => (
        <section className="card" key={group}>
          <h2>{GROUP_LABELS[group]}</h2>
          {FIELDS.filter((f) => f.group === group).map((def) => {
            const error = parsed.errors[def.key];
            const id = `field-${def.key}`;
            return (
              <div className={`field${error ? ' invalid' : ''}`} key={def.key}>
                <label htmlFor={id}>{def.label}</label>
                {def.kind === 'bool' ? (
                  <input
                    id={id}
                    type="checkbox"
                    checked={draft[def.key] === 'true'}
                    onChange={(e) => update(def.key, String(e.target.checked))}
                  />
                ) : (
                  <div className="input-row">
                    <input
                      id={id}
                      type="number"
                      inputMode={def.kind === 'int' ? 'numeric' : 'decimal'}
                      step={def.kind === 'int' ? 1 : def.kind === 'percent' ? 0.1 : 0.05}
                      value={draft[def.key]}
                      onChange={(e) => update(def.key, e.target.value)}
                    />
                    {def.unit && <span className="unit">{def.unit}</span>}
                  </div>
                )}
                <p className="help">{def.help} <span className="range">허용 {rangeText(def)}</span></p>
                {error && <p className="error">{error}</p>}
              </div>
            );
          })}
        </section>
      ))}

      {problems.length > 0 && (
        <section className="card">
          {problems.map((text) => (
            <p className="error" key={text}>{text}</p>
          ))}
        </section>
      )}

      <section className="card">
        <h2>고정 규칙 (웹에서 바꿀 수 없음)</h2>
        <ul>
          {READ_ONLY_RULES.map((rule) => (
            <li key={rule}>{rule}</li>
          ))}
        </ul>
      </section>

      <div className="actions">
        <button type="button" className="ghost" onClick={() => setDraft(toDraft(DEFAULTS))} disabled={saving}>
          기본값으로
        </button>
        <button type="button" onClick={askSave} disabled={!canSave}>
          저장…
        </button>
      </div>

      {message && <p className={message.kind === 'ok' ? 'notice ok' : 'notice warn'}>{message.text}</p>}

      {pending && (
        <div className="sheet" role="dialog" aria-modal="true" aria-labelledby="confirm-title">
          <div className="sheet-body">
            <h2 id="confirm-title">변경 내용 확인</h2>
            {pending.changes.length === 0 ? (
              <p className="muted">바뀐 항목이 없습니다.</p>
            ) : (
              <ul className="changes">
                {pending.changes.map((c) => (
                  <li key={c.key}>
                    <span>{fieldDef(c.key).label}</span>
                    <span className="change-value">{c.before} → <strong>{c.after}</strong></span>
                  </li>
                ))}
              </ul>
            )}
            {pending.changes.some((c) => isStrategyKey(c.key)) && (
              <p className="notice warn">전략 값을 바꿨습니다. 검증 기록과 함께 확인하세요.</p>
            )}
            <p className="muted">저장하면 다음 세션부터 적용됩니다. 지금 보유 중인 포지션에는 영향이 없습니다.</p>
            <div className="actions">
              <button type="button" className="ghost" onClick={() => setPending(null)} disabled={saving}>취소</button>
              <button type="button" onClick={confirmSave} disabled={saving}>{saving ? '저장 중…' : '저장'}</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

// 기준 문서를 Settings 형식으로 바꿉니다. 변경 내역 계산에만 씁니다.
function baseSettings(stored: StoredSettings): Settings {
  const { version: _version, updated_by: _by, ...settings } = stored;
  return settings as Settings;
}
