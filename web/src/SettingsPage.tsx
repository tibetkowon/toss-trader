import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
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
  type FieldDef,
  type Group,
  type Key,
  type Settings,
} from './limits';
import {
  ConflictError,
  type AppliedConfig,
  type AppliedSource,
  type SettingsStore,
  type StoredSettings,
} from './data';
import { LockIcon } from './icons';

const GROUPS: Group[] = ['risk', 'strategy', 'screener', 'ops'];

const GROUP_HINTS: Record<Group, string> = {
  risk: '손실을 제한하는 값입니다.',
  strategy: '진입 조건과 추세 필터입니다.',
  screener: '감시할 종목을 고르는 방식입니다.',
  ops: '조회와 기록 주기, 마감 시점입니다.',
};

interface Props {
  store: SettingsStore;
  user: { uid: string };
  loadApplied?: AppliedSource;
}

type NoticeTone = 'ok' | 'warn' | 'error';

function sameDraft(a: Draft, b: Draft): boolean {
  return FIELDS.every((f) => a[f.key] === b[f.key]);
}

function baseSettings(stored: StoredSettings): Settings {
  const { version: _version, updated_by: _by, ...settings } = stored;
  return settings as Settings;
}

interface Status {
  tone: 'ok' | 'warn' | 'error' | 'muted';
  label: string;
}

// 저장된 버전과 실행 중인 버전을 비교해 상태 문구를 고릅니다.
function statusOf(enabled: boolean, failed: boolean, applied: AppliedConfig | null, storedVersion: number | null): Status {
  if (!enabled) return { tone: 'muted', label: '실행 상태를 확인하지 않음' };
  if (failed) return { tone: 'error', label: '실행 상태를 가져오지 못함' };
  if (!applied) return { tone: 'muted', label: '실행 상태 확인 중' };
  if (applied.version === null) return { tone: 'muted', label: '버전 정보가 없는 트레이더' };
  if (storedVersion !== null && storedVersion > applied.version) {
    return { tone: 'warn', label: '다음 세션부터 적용 대기' };
  }
  return { tone: 'ok', label: '실행 중인 설정과 같음' };
}

export function SettingsPage({ store, user, loadApplied }: Props) {
  const [stored, setStored] = useState<StoredSettings | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [denied, setDenied] = useState(false);
  const [base, setBase] = useState<StoredSettings | null>(null);
  const [draft, setDraft] = useState<Draft>(toDraft(DEFAULTS));
  const [pending, setPending] = useState<{ values: Settings; changes: Change[] } | null>(null);
  const [notice, setNotice] = useState<{ tone: NoticeTone; text: string } | null>(null);
  const [saving, setSaving] = useState(false);
  const [applied, setApplied] = useState<AppliedConfig | null>(null);
  const [appliedFailed, setAppliedFailed] = useState(false);

  useEffect(
    () =>
      store.watch(
        (next) => {
          setLoaded(true);
          setStored(next);
        },
        (err) => {
          if (err.code === 'permission-denied') {
            setDenied(true);
          } else {
            setNotice({ tone: 'error', text: '설정을 불러오지 못했습니다. 잠시 후 다시 시도하세요.' });
          }
        },
      ),
    [store],
  );

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
    if (!loadApplied) return;
    let cancelled = false;
    const load = () =>
      loadApplied()
        .then((value) => {
          if (!cancelled) {
            setApplied(value);
            setAppliedFailed(false);
          }
        })
        .catch(() => {
          if (!cancelled) setAppliedFailed(true);
        });
    load();
    const timer = window.setInterval(load, 60_000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [loadApplied]);

  const parsed = useMemo(() => parseDraft(draft), [draft]);
  const hasFieldErrors = Object.keys(parsed.errors).length > 0;
  const problems = useMemo(() => (hasFieldErrors ? [] : crossErrors(parsed.values)), [hasFieldErrors, parsed]);
  const baseDraft = base ? toDraft(base) : null;
  const changedKeys = baseDraft ? FIELDS.filter((f) => draft[f.key] !== baseDraft[f.key]).map((f) => f.key) : [];
  const outdated = base !== null && source.version !== base.version;
  const canSave = changedKeys.length > 0 && !outdated && !saving && !hasFieldErrors && problems.length === 0;
  const status = statusOf(Boolean(loadApplied), appliedFailed, applied, stored?.version ?? null);

  const update = (key: Key, text: string) => {
    setNotice(null);
    setDraft((prev) => ({ ...prev, [key]: text }));
  };

  const askSave = () => {
    setNotice(null);
    setPending({ values: parsed.values, changes: base ? diffSettings(baseSettings(base), parsed.values) : [] });
  };

  const cancelSheet = useCallback(() => setPending(null), []);

  const confirmSave = async () => {
    if (!pending || !base) return;
    setSaving(true);
    try {
      const version = await store.save(user.uid, base.version, pending.values);
      setPending(null);
      setNotice({ tone: 'ok', text: `버전 ${version}로 저장했습니다. 다음 세션부터 적용됩니다.` });
    } catch (err) {
      setPending(null);
      setNotice({
        tone: 'error',
        text:
          err instanceof ConflictError
            ? `다른 곳에서 버전 ${err.current}이 먼저 저장되었습니다. 다시 불러온 뒤 편집하세요.`
            : '저장에 실패했습니다. 연결 상태를 확인한 뒤 다시 시도하세요.',
      });
    } finally {
      setSaving(false);
    }
  };

  const reload = () => {
    setBase(source);
    setDraft(toDraft(source));
    setNotice(null);
  };

  if (denied) {
    return (
      <section className="card empty">
        <span className="empty-icon">
          <LockIcon size={22} />
        </span>
        <h2>접근 권한이 없습니다</h2>
        <p className="muted">
          이 계정으로는 설정을 볼 수 없습니다. 아래 UID를 관리자 목록(<code>admins</code> 컬렉션)에 문서 ID로 추가한 뒤
          다시 열어 주세요.
        </p>
        <code className="uid">{user.uid}</code>
      </section>
    );
  }

  if (!loaded) {
    return <SettingsSkeleton />;
  }

  return (
    <>
      <section className="card summary" aria-label="설정 상태">
        <div className="versions">
          <div>
            <span className="label">저장된 설정</span>
            <strong>{stored ? `v${stored.version}` : '없음'}</strong>
          </div>
          <div>
            <span className="label">실행 중</span>
            <strong>{applied?.version != null ? `v${applied.version}` : '—'}</strong>
          </div>
        </div>
        <span className={`pill ${status.tone}`}>
          <span className="dot" aria-hidden="true" />
          {status.label}
        </span>
        {!stored && (
          <p className="muted small">저장된 설정이 아직 없습니다. 값을 확인하고 저장하면 버전 1이 만들어집니다.</p>
        )}
      </section>

      {outdated && (
        <Notice
          tone="warn"
          action={
            <button type="button" className="btn ghost small" onClick={reload}>
              다시 불러오기
            </button>
          }
        >
          다른 곳에서 새 버전이 저장되었습니다. 편집 내용을 버리고 최신 값을 불러오세요.
        </Notice>
      )}
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}

      {GROUPS.map((group) => {
        const fields = FIELDS.filter((f) => f.group === group);
        return (
          <section className="card" key={group} aria-labelledby={`group-${group}`}>
            <header className="card-head">
              <div>
                <h2 id={`group-${group}`}>{GROUP_LABELS[group]}</h2>
                <p className="muted small">{GROUP_HINTS[group]}</p>
              </div>
              <span className="count">{fields.length}개</span>
            </header>
            {fields.map((def) => (
              <Field
                key={def.key}
                def={def}
                value={draft[def.key]}
                error={parsed.errors[def.key]}
                onChange={(text) => update(def.key, text)}
              />
            ))}
          </section>
        );
      })}

      {problems.length > 0 && <Notice tone="error">{problems.join(' · ')}</Notice>}

      <section className="card">
        <header className="card-head">
          <div>
            <h2>웹에서 바꾸지 않는 규칙</h2>
            <p className="muted small">안전을 위해 코드에 고정되어 있습니다.</p>
          </div>
          <span className="lock-badge">
            <LockIcon size={16} />
          </span>
        </header>
        <ul className="rules">
          {READ_ONLY_RULES.map((rule) => (
            <li key={rule}>{rule}</li>
          ))}
        </ul>
      </section>

      <div className="actionbar">
        <div className="actionbar-inner">
          <p className="changed" aria-live="polite">
            {changedKeys.length > 0 ? (
              <>
                변경 <strong>{changedKeys.length}</strong>개
              </>
            ) : (
              '변경 없음'
            )}
          </p>
          <div className="btns">
            <button
              type="button"
              className="btn ghost"
              onClick={() => {
                setNotice(null);
                setDraft(toDraft(DEFAULTS));
              }}
              disabled={saving}
            >
              기본값
            </button>
            <button type="button" className="btn primary" onClick={askSave} disabled={!canSave}>
              저장
            </button>
          </div>
        </div>
      </div>

      {pending && (
        <ConfirmSheet changes={pending.changes} saving={saving} onCancel={cancelSheet} onConfirm={confirmSave} />
      )}
    </>
  );
}

function Notice({ tone, children, action }: { tone: NoticeTone; children: ReactNode; action?: ReactNode }) {
  return (
    <div className={`notice ${tone}`} role={tone === 'error' ? 'alert' : 'status'}>
      <div className="notice-text">{children}</div>
      {action}
    </div>
  );
}

interface FieldProps {
  def: FieldDef;
  value: string;
  error?: string;
  onChange: (text: string) => void;
}

function Field({ def, value, error, onChange }: FieldProps) {
  const id = `field-${def.key}`;
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const isBool = def.kind === 'bool';

  return (
    <div className={`field${error ? ' invalid' : ''}`}>
      <div className="field-main">
        <div className="field-text">
          <label htmlFor={id}>{def.label}</label>
          <p className="hint" id={hintId}>
            {def.help}
          </p>
          {!isBool && <p className="range">허용 {rangeText(def)}</p>}
        </div>
        {isBool ? (
          <input
            id={id}
            className="switch"
            type="checkbox"
            role="switch"
            aria-describedby={hintId}
            checked={value === 'true'}
            onChange={(e) => onChange(String(e.target.checked))}
          />
        ) : (
          <div className="control">
            <input
              id={id}
              type="number"
              inputMode={def.kind === 'int' ? 'numeric' : 'decimal'}
              step={def.kind === 'int' ? 1 : def.kind === 'percent' ? 0.1 : 0.05}
              value={value}
              aria-invalid={Boolean(error)}
              aria-describedby={error ? `${hintId} ${errorId}` : hintId}
              onChange={(e) => onChange(e.target.value)}
            />
            {def.unit && <span className="suffix">{def.unit}</span>}
          </div>
        )}
      </div>
      {error && (
        <p className="error" id={errorId}>
          {error}
        </p>
      )}
    </div>
  );
}

function ConfirmSheet({
  changes,
  saving,
  onCancel,
  onConfirm,
}: {
  changes: Change[];
  saving: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    cancelRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !saving) onCancel();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onCancel, saving]);

  const touchesStrategy = changes.some((c) => isStrategyKey(c.key));

  return (
    <div className="backdrop" onClick={() => !saving && onCancel()}>
      <div
        className="sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sheet-handle" aria-hidden="true" />
        <h2 id="confirm-title">변경 내용 확인</h2>
        {changes.length === 0 ? (
          <p className="muted">바뀐 항목이 없습니다.</p>
        ) : (
          <ul className="changes">
            {changes.map((c) => (
              <li key={c.key}>
                <span className="change-name">{fieldDef(c.key).label}</span>
                <span className="change-value">
                  <s>{c.before}</s> → <strong>{c.after}</strong>
                </span>
              </li>
            ))}
          </ul>
        )}
        {touchesStrategy && <Notice tone="warn">전략 값을 바꿨습니다. 검증 기록과 함께 확인하세요.</Notice>}
        <p className="muted small">저장하면 다음 세션부터 적용됩니다. 지금 보유 중인 포지션에는 영향이 없습니다.</p>
        <div className="sheet-actions">
          <button ref={cancelRef} type="button" className="btn ghost" onClick={onCancel} disabled={saving}>
            취소
          </button>
          <button type="button" className="btn primary" onClick={onConfirm} disabled={saving}>
            {saving ? '저장 중…' : '저장'}
          </button>
        </div>
      </div>
    </div>
  );
}

function SettingsSkeleton() {
  return (
    <div aria-busy="true" aria-label="설정을 불러오는 중">
      <section className="card">
        <span className="bar w40" />
        <span className="bar w70" />
        <span className="bar w55" />
      </section>
      <section className="card">
        <span className="bar w40" />
        <span className="bar w70" />
      </section>
    </div>
  );
}
