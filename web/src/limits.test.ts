import { describe, expect, it } from 'vitest';
import { DEFAULTS, crossErrors, diffSettings, parseDraft, toDraft, type Settings } from './limits';

describe('기본값', () => {
  it('검증을 통과하고 화면 값으로 왕복합니다', () => {
    const { values, errors } = parseDraft(toDraft(DEFAULTS));
    expect(errors).toEqual({});
    expect(crossErrors(values)).toEqual([]);
    expect(values).toEqual(DEFAULTS);
  });

  it('손절 2%는 화면에서 2로 보입니다', () => {
    expect(toDraft(DEFAULTS).stop_loss_pct).toBe('2');
  });
});

describe('손절·일일 한도 범위', () => {
  const withDraft = (key: keyof Settings, text: string) => {
    const draft = toDraft(DEFAULTS);
    draft[key] = text;
    return parseDraft(draft).errors;
  };

  it('1~2% 사이의 손절은 허용합니다', () => {
    expect(withDraft('stop_loss_pct', '1')).toEqual({});
    expect(withDraft('stop_loss_pct', '2')).toEqual({});
  });

  it('2%보다 느슨한 손절과 1% 미만은 거부합니다', () => {
    expect(withDraft('stop_loss_pct', '2.5')).toHaveProperty('stop_loss_pct');
    expect(withDraft('stop_loss_pct', '0.5')).toHaveProperty('stop_loss_pct');
  });

  it('1~5% 사이의 일일 한도는 허용하고 5%를 넘으면 거부합니다', () => {
    expect(withDraft('daily_loss_limit_pct', '1')).toEqual({});
    expect(withDraft('daily_loss_limit_pct', '5')).toEqual({});
    expect(withDraft('daily_loss_limit_pct', '5.1')).toHaveProperty('daily_loss_limit_pct');
  });
});

describe('입력 형식', () => {
  it('정수 항목에 소수를 넣으면 거부합니다', () => {
    const draft = toDraft(DEFAULTS);
    draft.active_count = '3.5';
    expect(parseDraft(draft).errors).toHaveProperty('active_count');
  });

  it('비어 있는 입력은 거부합니다', () => {
    const draft = toDraft(DEFAULTS);
    draft.k = '';
    expect(parseDraft(draft).errors).toHaveProperty('k');
  });
});

describe('항목 사이 관계', () => {
  it('노이즈 하한이 상한 이상이면 거부합니다', () => {
    const values = { ...DEFAULTS, noise_min: 0.06, noise_max: 0.06 };
    expect(crossErrors(values)).toHaveLength(1);
  });

  it('활성 종목 수가 랭킹 깊이를 넘으면 거부합니다', () => {
    const values = { ...DEFAULTS, active_count: 31, rank_depth: 30 };
    expect(crossErrors(values)).toHaveLength(1);
  });
});

describe('변경 내역', () => {
  it('바뀐 항목만 화면 표시 값으로 돌려줍니다', () => {
    const next = { ...DEFAULTS, stop_loss_pct: 0.015, lazy_expand: false };
    const changes = diffSettings(DEFAULTS, next);
    expect(changes.map((c) => c.key)).toEqual(['stop_loss_pct', 'lazy_expand']);
    expect(changes[0]).toEqual({ key: 'stop_loss_pct', before: '2%', after: '1.5%' });
    expect(changes[1]).toEqual({ key: 'lazy_expand', before: '켬', after: '끔' });
  });
});
