// 설정 항목의 정의와 범위입니다. 값은 internal/settings(Go), firebase/firestore.rules와 같아야 합니다.
// 비율 항목(percent)은 저장할 때 소수(0.02 = 2%)이고, 화면에서는 %로 보여 줍니다.

export interface Settings {
  stop_loss_pct: number;
  daily_loss_limit_pct: number;
  k: number;
  ma_window: number;
  noise_window: number;
  noise_min: number;
  noise_max: number;
  min_affordable: number;
  rank_depth: number;
  active_count: number;
  active_keep_rank: number;
  eval_per_min: number;
  lazy_expand: boolean;
  rank_start_delay_minutes: number;
  rank_refresh_seconds: number;
  chase_limit_pct: number;
  poll_interval_seconds: number;
  heartbeat_interval_seconds: number;
  eod_buffer_minutes: number;
}

export type Key = keyof Settings;
export type Draft = Record<Key, string>;
export type Group = 'risk' | 'strategy' | 'screener' | 'ops';
export type Kind = 'percent' | 'decimal' | 'int' | 'bool';

export interface FieldDef {
  key: Key;
  label: string;
  group: Group;
  kind: Kind;
  min: number; // 저장 단위(percent면 소수)
  max: number;
  unit: string;
  help: string;
}

export const GROUP_LABELS: Record<Group, string> = {
  risk: '리스크',
  strategy: '전략',
  screener: '종목 선정',
  ops: '운영 주기',
};

export const FIELDS: readonly FieldDef[] = [
  { key: 'stop_loss_pct', label: '손절 폭', group: 'risk', kind: 'percent', min: 0.01, max: 0.02, unit: '%',
    help: '매수가 대비 이만큼 떨어지면 손절합니다. 2%보다 느슨하게는 설정할 수 없습니다.' },
  { key: 'daily_loss_limit_pct', label: '일일 손실 한도 (시드 대비)', group: 'risk', kind: 'percent', min: 0.01, max: 0.05, unit: '%',
    help: '하루 손실이 이 비율을 넘으면 당일 매매를 멈춥니다. 5%보다 느슨하게는 설정할 수 없습니다.' },
  { key: 'k', label: '돌파 계수 k', group: 'strategy', kind: 'decimal', min: 0.1, max: 1.0, unit: '',
    help: '목표가 = 오늘 시가 + (전일 고가 − 전일 저가) × k' },
  { key: 'ma_window', label: '추세 필터 이동평균 기간', group: 'strategy', kind: 'int', min: 2, max: 20, unit: '일',
    help: '전일 종가가 이 기간의 이동평균 위에 있을 때만 매수합니다.' },
  { key: 'noise_window', label: '노이즈 계산 기간', group: 'strategy', kind: 'int', min: 5, max: 60, unit: '일',
    help: '(고가 − 저가) / 시가의 평균을 낼 기간입니다.' },
  { key: 'noise_min', label: '노이즈 하한', group: 'strategy', kind: 'percent', min: 0.005, max: 0.2, unit: '%',
    help: '이 값보다 변동이 작은 종목은 제외합니다.' },
  { key: 'noise_max', label: '노이즈 상한', group: 'strategy', kind: 'percent', min: 0.005, max: 0.2, unit: '%',
    help: '이 값보다 변동이 큰 종목은 제외합니다. 하한보다 커야 합니다.' },
  { key: 'min_affordable', label: '살 수 있는 종목 최소 개수 (국장)', group: 'strategy', kind: 'int', min: 0, max: 10, unit: '개',
    help: '현재 시드로 1주라도 살 수 있는 종목이 이 개수 이상 있어야 합니다. 미국장에는 적용하지 않습니다.' },
  { key: 'rank_depth', label: '장중 랭킹 조회 깊이', group: 'screener', kind: 'int', min: 1, max: 100, unit: '위',
    help: '거래대금 랭킹 상위 몇 개까지 조회할지 정합니다.' },
  { key: 'active_count', label: '활성 종목 수', group: 'screener', kind: 'int', min: 1, max: 30, unit: '개',
    help: '감시하고 진입할 종목 수입니다. 랭킹 조회 깊이 이하여야 합니다.' },
  { key: 'active_keep_rank', label: '유지 순위 (히스테리시스)', group: 'screener', kind: 'int', min: 0, max: 100, unit: '위',
    help: '한 번 활성이 된 종목은 이 순위 밖으로 밀릴 때까지 유지합니다. 0이면 끕니다.' },
  { key: 'eval_per_min', label: '분당 신규 평가 상한', group: 'screener', kind: 'int', min: 0, max: 60, unit: '종목',
    help: '새로 등장한 종목을 분당 몇 개까지 평가할지 정합니다(429 방지). 0이면 제한이 없습니다.' },
  { key: 'lazy_expand', label: '장중 후보 확장 사용', group: 'screener', kind: 'bool', min: 0, max: 1, unit: '',
    help: '끄면 첫 갱신 때의 랭킹 상위 종목으로 후보를 고정합니다.' },
  { key: 'rank_start_delay_minutes', label: '개장 후 첫 편입까지', group: 'screener', kind: 'int', min: 0, max: 60, unit: '분',
    help: '개장 직후 급등 종목을 피하려고 기다리는 시간입니다.' },
  { key: 'rank_refresh_seconds', label: '활성 종목 갱신 주기', group: 'screener', kind: 'int', min: 10, max: 600, unit: '초',
    help: '장중 랭킹으로 활성 종목을 다시 고르는 간격입니다.' },
  { key: 'chase_limit_pct', label: '추격 상한', group: 'screener', kind: 'percent', min: 0, max: 0.05, unit: '%',
    help: '현재가가 목표가를 이 비율 넘게 앞지르면 그날은 진입하지 않습니다. 0이면 끕니다.' },
  { key: 'poll_interval_seconds', label: '시세 폴링 주기', group: 'ops', kind: 'int', min: 2, max: 60, unit: '초',
    help: '보유·활성 종목 시세를 조회하는 간격입니다.' },
  { key: 'heartbeat_interval_seconds', label: '상태 스냅샷 갱신 주기', group: 'ops', kind: 'int', min: 10, max: 600, unit: '초',
    help: '변화가 없어도 화면용 상태 파일을 다시 쓰는 간격입니다.' },
  { key: 'eod_buffer_minutes', label: '마감 강제청산 시작 시점', group: 'ops', kind: 'int', min: 0, max: 60, unit: '분 전',
    help: '정규장 종료 몇 분 전에 보유 포지션을 정리할지 정합니다.' },
];

// 웹에서 바꿀 수 없는 구조 규칙입니다(SPEC.md 3.1, 4.2, 4.3). 화면에는 참고용으로만 보여 줍니다.
export const READ_ONLY_RULES: readonly string[] = [
  '연속 손실 2회면 해당 거래일 신규 매수 중단',
  '동시 보유 1종목, 당일 재진입 금지',
  '시장가 주문',
];

export const DEFAULTS: Settings = {
  stop_loss_pct: 0.02,
  daily_loss_limit_pct: 0.05,
  k: 0.5,
  ma_window: 5,
  noise_window: 20,
  noise_min: 0.025,
  noise_max: 0.06,
  min_affordable: 2,
  rank_depth: 30,
  active_count: 10,
  active_keep_rank: 30,
  eval_per_min: 3,
  lazy_expand: true,
  rank_start_delay_minutes: 5,
  rank_refresh_seconds: 60,
  chase_limit_pct: 0.01,
  poll_interval_seconds: 4,
  heartbeat_interval_seconds: 60,
  eod_buffer_minutes: 15,
};

export function fieldDef(key: Key): FieldDef {
  const def = FIELDS.find((f) => f.key === key);
  if (!def) throw new Error(`알 수 없는 설정 항목: ${key}`);
  return def;
}

// 소수점 오차를 없애려고 소수 여섯 자리에서 맞춥니다.
function toPercentText(value: number): string {
  return String(Number((value * 100).toFixed(4)));
}

export function toDraft(settings: Settings): Draft {
  const draft = {} as Draft;
  for (const def of FIELDS) {
    const value = settings[def.key];
    draft[def.key] = def.kind === 'bool'
      ? String(value)
      : def.kind === 'percent'
        ? toPercentText(value as number)
        : String(value);
  }
  return draft;
}

// 입력 문자열을 숫자로 바꿉니다. 비어 있거나 숫자가 아니면 NaN입니다.
export function parseDraft(draft: Draft): { values: Settings; errors: Partial<Record<Key, string>> } {
  const values = {} as Record<string, unknown>;
  const errors: Partial<Record<Key, string>> = {};
  for (const def of FIELDS) {
    const text = draft[def.key].trim();
    if (def.kind === 'bool') {
      values[def.key] = text === 'true';
      continue;
    }
    const raw = text === '' ? Number.NaN : Number(text);
    const value = def.kind === 'percent' ? Number((raw / 100).toFixed(6)) : raw;
    values[def.key] = value;
    const problem = checkField(def, value);
    if (problem) errors[def.key] = problem;
  }
  return { values: values as unknown as Settings, errors };
}

export function rangeText(def: FieldDef): string {
  if (def.kind === 'percent') {
    return `${(def.min * 100).toFixed(1)}~${(def.max * 100).toFixed(1)}%`;
  }
  return `${def.min}~${def.max}${def.unit}`;
}

function checkField(def: FieldDef, value: number): string | undefined {
  if (!Number.isFinite(value)) return '숫자를 입력하세요';
  if (def.kind === 'int' && !Number.isInteger(value)) return '정수로 입력하세요';
  if (value < def.min || value > def.max) return `허용 범위는 ${rangeText(def)}입니다`;
  return undefined;
}

// 항목 사이의 관계(Go Settings.Validate와 같은 규칙)를 검사합니다.
export function crossErrors(values: Settings): string[] {
  const problems: string[] = [];
  if (values.noise_min >= values.noise_max) problems.push('노이즈 하한은 노이즈 상한보다 작아야 합니다');
  if (values.active_count > values.rank_depth) problems.push('활성 종목 수는 랭킹 조회 깊이 이하여야 합니다');
  return problems;
}

export interface Change {
  key: Key;
  before: string;
  after: string;
}

// 두 설정의 차이를 화면 표시용 문자열로 돌려줍니다.
export function diffSettings(before: Settings, after: Settings): Change[] {
  const before_ = toDraft(before);
  const after_ = toDraft(after);
  return FIELDS.filter((def) => before_[def.key] !== after_[def.key]).map((def) => ({
    key: def.key,
    before: display(def, before_[def.key]),
    after: display(def, after_[def.key]),
  }));
}

export function display(def: FieldDef, text: string): string {
  if (def.kind === 'bool') return text === 'true' ? '켬' : '끔';
  return def.kind === 'percent' ? `${text}%` : `${text}${def.unit ? ` ${def.unit}` : ''}`;
}

export function isStrategyKey(key: Key): boolean {
  return fieldDef(key).group === 'strategy';
}
