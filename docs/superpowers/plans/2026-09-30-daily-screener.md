# 일일 종목 스크리너 구현 계획 (라이브 경로)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 고정 8종목 워치리스트를 없애고, 실시간 당일 거래대금 랭킹에서 매일 활성 종목 10개를 뽑아 감시·진입하는 스크리너를 `cmd/trader`에 붙인다.

**Architecture:** 새 `internal/screener`가 종목별 평가(필터, 목표가, 추세)와 하루 캐시(`Evaluator`), 랭킹 기반 활성 집합 선택(`SelectActive` 순수 함수 + `Gate` 러너)을 맡는다. 시뮬레이터는 손대지 않고, 추격 상한은 `tradingloop.ChaseGuard`가 진입 전 관측값을 걸러서 처리한다(보유 종목 청산 경로는 절대 막지 않는다). 평가 캐시는 `internal/session`에 저장해 장중 재시작 시 재평가하지 않는다.

**Tech Stack:** Go 1.27, `modernc.org/sqlite`, 표준 라이브러리 `testing`(가짜 `Source` 사용, 실 API 호출 없음).

**Spec:** `docs/superpowers/specs/2026-09-30-daily-screener-design.md` (SPEC.md v15 3.3절이 요약)

**범위 밖(별도 계획):** `cmd/backtest`의 스크리너 재구성·A/B 실험(설계 6절), 미장 세션 복구. 이 계획이 끝나 `screener` 인터페이스가 실제로 굳은 뒤에 백테스트 계획을 따로 쓴다. 이 계획에서 `cmd/backtest`는 옛 고정 리스트를 로컬 변수로 옮겨 계속 컴파일되게만 한다.

## Global Constraints

- 모듈 경로 `github.com/tibetkowon/toss-trader`, 테스트는 `go test ./...`, 포맷은 `gofmt`.
- 전략 파라미터 고정: 목표가 = 오늘 시가 + 0.5 × (전일 고가 − 전일 저가), MA5 추세 필터(어제까지 5일 종가), 손절 2%, 일일 손실 한도 5%, 연속 2패 정지, 동시 보유 1종목 전액, 마감 15분 전 강제청산, 폴링 4초, 시세 30초 초과 stale 무시, 하트비트 60초.
- 평가는 **어제까지 확정된 일봉만** 쓴다(오늘 형성 중인 봉은 `strategy.SplitBars`로 분리). 오늘 시가는 정규장 첫 1분봉의 시가.
- 노이즈 = 직전 20일 평균 (고가−저가)/시가, 초기 구간 2.5%~6%.
- 초기 설정값: `RANK_DEPTH`=30, `ACTIVE_COUNT`=10, `RANK_START_DELAY`=5분, `RANK_REFRESH`=60초, `EVAL_PER_MIN`=3, `ACTIVE_KEEP_RANK`=0(끔), `CHASE_LIMIT_PCT`=1%, 최소 살 수 있는 종목 2개.
- 랭킹은 `Rankings(ctx, "MARKET_TRADING_AMOUNT", "1d", market)`, 순위는 `RankingItem.Rank` 필드로 판단한다(응답 순서를 믿지 않는다).
- 호출량: 랭킹 분당 1회, 신규 종목 정적 평가는 분당 `EVAL_PER_MIN`개, 활성 시세는 4초마다 최대 10건. `tossapi.Client`가 429 대기를 이미 처리한다.
- 모든 요소는 환경변수로 켜고 끌 수 있어야 한다(지연 0, 히스테리시스 0, 추격 상한 0, 장중 확장 끔).
- 실제 GCS 버킷 이름·시크릿은 커밋 금지.
- 커밋 메시지 끝에 `Co-Authored-By: Claude Code <noreply@anthropic.com>`.

## Review Focus

- 랭킹이 비었거나 평가 통과 종목이 10개 미만 → 활성 집합이 작아질 뿐 패닉·무한 대기 없음 (Task 6).
- 랭킹에는 있는데 `Stocks`가 빈 배열을 돌려주는 종목 → 탈락으로 캐시, 패닉 없음 (Task 3).
- 보유 종목이 랭킹·활성 집합에서 밀려나도 계속 폴링되어 손절·마감 청산이 동작 (Task 10).
- 장중 재시작 시 저장된 평가 캐시로 복구되어 재조회하지 않고, 보유 포지션이 그대로 관리됨 (Task 8, 10).
- `leverageFactor`가 문자열/숫자/null/누락 어느 형태로 와도 오판정하지 않음, 랭킹 가격이 `"0"`·비숫자여도 크래시 없음 (Task 1, 5).
- 추격 상한이 보유 종목 청산 경로를 막지 않고, stale 시세로 종목을 오차단하지 않음 (Task 7).

## 파일 구조

| 파일 | 역할 |
|---|---|
| `internal/tossapi/marketdata.go` (수정) | `Stock.IsLeveraged()` |
| `internal/screener/screener.go` (신규) | `Source`, `Config`, `Entry`, 일봉 변환, 노이즈 계산 |
| `internal/screener/evaluator.go` (신규) | `Evaluator`: 정적 평가(1~3단계), 시가 확정(4단계), 캐시, 분당 예산 |
| `internal/screener/select.go` (신규) | `SelectActive` 순수 함수 (히스테리시스, 최소 살 수 있는 종목) |
| `internal/screener/gate.go` (신규) | `Gate`: 아침 사전 평가, 장중 갱신, 실패 시 대체 |
| `internal/screener/fake_test.go` (신규) | 테스트용 가짜 `Source`와 헬퍼 |
| `internal/tradingloop/chase.go` (신규) | `ChaseGuard` |
| `internal/session/session.go` (수정) | 평가 캐시 저장/복구 |
| `internal/snapshot/snapshot.go` (수정) | 대시보드에 활성 종목·탈락 사유 |
| `cmd/trader/main.go`, `cmd/trader/screener.go` (수정/신규) | 통합, 환경변수 설정 |
| `internal/strategy/watchlist*.go` → `cmd/backtest/watchlist.go` | 옛 리스트를 백테스트로 이동 |
| `SPEC.md`, 메모리 | 반영 |

---

### Task 1: `Stock.IsLeveraged()` 관대한 파서

**Files:**
- Modify: `internal/tossapi/marketdata.go` (Stock 정의 바로 아래에 메서드 추가)
- Test: `internal/tossapi/marketdata_test.go`

**Interfaces:**
- Produces: `func (s Stock) IsLeveraged() bool` — `Raw`의 `leverageFactor`가 없음/null/`""`/`1`/`"1"`/`1.0`이면 false, 그 외 숫자(3, -2, "3")이거나 숫자로 해석 불가한 비어 있지 않은 값이면 true(보수적으로 제외).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/tossapi/marketdata_test.go` 끝에 추가:

```go
func TestStockIsLeveraged(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"필드 없음", `{"symbol":"A"}`, false},
		{"null", `{"leverageFactor":null}`, false},
		{"빈 문자열", `{"leverageFactor":""}`, false},
		{"문자열 1", `{"leverageFactor":"1"}`, false},
		{"숫자 1", `{"leverageFactor":1}`, false},
		{"숫자 1.0", `{"leverageFactor":1.0}`, false},
		{"문자열 3배", `{"leverageFactor":"3"}`, true},
		{"숫자 3배", `{"leverageFactor":3}`, true},
		{"인버스 -1", `{"leverageFactor":"-1"}`, true},
		{"해석 불가 문자열", `{"leverageFactor":"x2"}`, true},
		{"원문 없음", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Stock{Raw: json.RawMessage(c.raw)}
			if got := s.IsLeveraged(); got != c.want {
				t.Errorf("IsLeveraged(%s) = %v, 기대 %v", c.raw, got, c.want)
			}
		})
	}
}
```

파일 상단 import에 `"encoding/json"`이 없으면 추가한다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/tossapi -run TestStockIsLeveraged`
Expected: FAIL (`IsLeveraged` 미정의로 컴파일 오류)

- [ ] **Step 3: 구현**

`internal/tossapi/marketdata.go`의 `Stock` 구조체 정의 뒤에 추가 (`strconv`, `strings`, `encoding/json`은 이미 import됨):

```go
// IsLeveraged는 레버리지/인버스 상품 여부를 원문의 leverageFactor로 판단합니다.
// 값이 문자열이든 숫자든 받아들이고, 해석할 수 없는 값은 보수적으로 true입니다.
func (s Stock) IsLeveraged() bool {
	var probe struct {
		LeverageFactor json.RawMessage `json:"leverageFactor"`
	}
	if err := json.Unmarshal(s.Raw, &probe); err != nil {
		return false
	}
	v := strings.Trim(strings.TrimSpace(string(probe.LeverageFactor)), `"`)
	if v == "" || v == "null" {
		return false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return true
	}
	return n != 1
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/tossapi`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/tossapi/marketdata.go internal/tossapi/marketdata_test.go
git commit -m "Add tolerant Stock.IsLeveraged parser for leverageFactor"
```

---

### Task 2: `screener` 기본 타입, 일봉 변환, 노이즈 계산

**Files:**
- Create: `internal/screener/screener.go`
- Create: `internal/screener/fake_test.go` (이후 태스크가 공유하는 테스트 헬퍼)
- Test: `internal/screener/screener_test.go`

**Interfaces:**
- Produces:
  - `type Source interface` (Rankings, Stocks, StockWarnings, Candles — `*tossapi.Client`가 그대로 만족)
  - `type Config struct`, `func DefaultConfig(market string) Config`
  - `type Entry struct` (JSON 태그 포함, 저장 가능), `func (e Entry) Setup() simulator.Setup`
  - `func BarsFromCandles([]tossapi.Candle) []strategy.DailyBar` — 최신순 유지, 가격 파싱 실패·0 이하 봉은 버림
  - `func NoiseRatio(prior []strategy.DailyBar, window int) (float64, bool)`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/screener/screener_test.go`:

```go
package screener

import (
	"math"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func TestBarsFromCandlesSkipsBadBars(t *testing.T) {
	candles := []tossapi.Candle{
		{Timestamp: "2026-09-30T00:00:00.000+09:00", OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-29", OpenPrice: "x", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "short", OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-26T00:00:00.000+09:00", OpenPrice: "0", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-25T00:00:00.000+09:00", OpenPrice: "90", HighPrice: "95", LowPrice: "89", ClosePrice: "94"},
	}
	got := BarsFromCandles(candles)
	if len(got) != 2 || got[0].Date != "2026-09-30" || got[1].Date != "2026-09-25" {
		t.Fatalf("변환 결과: %+v", got)
	}
	if got[1].Open != 90 || got[1].High != 95 || got[1].Low != 89 || got[1].Close != 94 {
		t.Errorf("값 불일치: %+v", got[1])
	}
}

func TestNoiseRatio(t *testing.T) {
	prior := []strategy.DailyBar{
		{Open: 100, High: 104, Low: 100}, // 4%
		{Open: 200, High: 208, Low: 200}, // 4%
		{Open: 100, High: 110, Low: 100}, // 10% — 윈도 밖
	}
	got, ok := NoiseRatio(prior, 2)
	if !ok || math.Abs(got-0.04) > 1e-9 {
		t.Fatalf("NoiseRatio = %v, %v", got, ok)
	}
	if _, ok := NoiseRatio(prior, 4); ok {
		t.Error("봉이 부족한데 ok=true")
	}
	if _, ok := NoiseRatio(prior, 0); ok {
		t.Error("window 0을 허용했습니다")
	}
}

func TestEntrySetup(t *testing.T) {
	e := Entry{Symbol: "A", Passed: true, HasOpen: true, Target: 105, TrendOK: true}
	s := e.Setup()
	if s.Symbol != "A" || s.TargetPrice != 105 || !s.TrendOK {
		t.Fatalf("Setup = %+v", s)
	}
}
```

`internal/screener/fake_test.go`:

```go
package screener

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

var kst = time.FixedZone("KST", 9*3600)

func testSessionStart() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, kst) }

func testConfig() Config {
	return Config{
		Market: "KR", NoiseMin: 0.025, NoiseMax: 0.06, NoiseWindow: 20, MAWindow: 5, K: 0.5,
		RankDepth: 5, ActiveCount: 2, MinAffordable: 0, EvalPerMin: 0,
		StartDelay: 5 * time.Minute, RefreshEvery: time.Minute, LazyExpand: true,
		Seed: 1_000_000, CommissionRate: 0,
	}
}

type fakeSource struct {
	rankings   []tossapi.RankingItem
	rankingErr error
	stocks     map[string]tossapi.Stock
	warnings   map[string][]json.RawMessage
	daily      map[string][]tossapi.Candle
	minute     map[string][]tossapi.Candle
	errOn      map[string]error // 키: "Stocks:SYM", "Warnings:SYM", "Daily:SYM", "Minute:SYM"
	calls      map[string]int
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		stocks: map[string]tossapi.Stock{}, warnings: map[string][]json.RawMessage{},
		daily: map[string][]tossapi.Candle{}, minute: map[string][]tossapi.Candle{},
		errOn: map[string]error{}, calls: map[string]int{},
	}
}

func (f *fakeSource) Rankings(ctx context.Context, rankingType, duration, market string) ([]tossapi.RankingItem, error) {
	f.calls["Rankings"]++
	return f.rankings, f.rankingErr
}

func (f *fakeSource) Stocks(ctx context.Context, symbols ...string) ([]tossapi.Stock, error) {
	f.calls["Stocks"]++
	if err := f.errOn["Stocks:"+symbols[0]]; err != nil {
		return nil, err
	}
	if s, ok := f.stocks[symbols[0]]; ok {
		return []tossapi.Stock{s}, nil
	}
	return nil, nil
}

func (f *fakeSource) StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error) {
	f.calls["Warnings"]++
	if err := f.errOn["Warnings:"+symbol]; err != nil {
		return nil, err
	}
	return f.warnings[symbol], nil
}

func (f *fakeSource) Candles(ctx context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error) {
	if interval == "1d" {
		f.calls["Daily"]++
		if err := f.errOn["Daily:"+symbol]; err != nil {
			return nil, "", err
		}
		return f.daily[symbol], "", nil
	}
	f.calls["Minute"]++
	if err := f.errOn["Minute:"+symbol]; err != nil {
		return nil, "", err
	}
	return f.minute[symbol], "", nil
}

// addStock은 평범한(레버리지 아님) 종목 정보를 등록합니다.
func (f *fakeSource) addStock(symbol string) {
	f.stocks[symbol] = tossapi.Stock{
		Symbol: symbol,
		Raw:    json.RawMessage(fmt.Sprintf(`{"symbol":%q,"leverageFactor":"1"}`, symbol)),
	}
}

// dailyCandles는 최신순 일봉 n개를 만듭니다. 직전 봉은 시 100 고 104 저 100 종 103
// (노이즈 4%, 추세 통과), 그 이전 봉은 종가 100입니다. withToday면 형성 중인 오늘 봉을 맨 앞에 넣습니다.
func dailyCandles(sessionDate string, n int, withToday bool) []tossapi.Candle {
	day, _ := time.Parse("2006-01-02", sessionDate)
	var out []tossapi.Candle
	if withToday {
		out = append(out, tossapi.Candle{
			Timestamp: sessionDate + "T00:00:00.000+09:00",
			OpenPrice: "110", HighPrice: "130", LowPrice: "109", ClosePrice: "125",
		})
	}
	for i := 1; i <= n; i++ {
		closePrice := "100"
		if i == 1 {
			closePrice = "103"
		}
		out = append(out, tossapi.Candle{
			Timestamp: day.AddDate(0, 0, -i).Format("2006-01-02") + "T00:00:00.000+09:00",
			OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: closePrice,
		})
	}
	return out
}

func minuteCandle(ts time.Time, open string) tossapi.Candle {
	return tossapi.Candle{Timestamp: ts.Format(time.RFC3339), OpenPrice: open, HighPrice: open, LowPrice: open, ClosePrice: open}
}

func rankingItem(rank int, symbol, price string) tossapi.RankingItem {
	return tossapi.RankingItem{Rank: rank, Symbol: symbol, Price: tossapi.RankingPrice{LastPrice: price}}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/screener`
Expected: FAIL (패키지에 `screener.go` 없음)

- [ ] **Step 3: 구현**

`internal/screener/screener.go`:

```go
// Package screener는 매일 활성 감시 종목을 고릅니다(설계: docs/superpowers/specs/
// 2026-09-30-daily-screener-design.md). 종목 평가는 어제까지 확정된 데이터로만 하고,
// 활성 집합은 실시간 당일 거래대금 랭킹으로 정합니다.
package screener

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

const dateLayout = "2006-01-02"

// Source는 스크리너가 쓰는 API 표면입니다. *tossapi.Client가 그대로 만족합니다.
type Source interface {
	Rankings(ctx context.Context, rankingType, duration, marketCountry string) ([]tossapi.RankingItem, error)
	Stocks(ctx context.Context, symbols ...string) ([]tossapi.Stock, error)
	StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error)
	Candles(ctx context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error)
}

var _ Source = (*tossapi.Client)(nil)

// Config의 0값 의미: EvalPerMin 0 = 제한 없음, StartDelay 0 = 지연 없음,
// KeepRank 0 = 히스테리시스 끔, MinAffordable 0 = 규칙 끔.
type Config struct {
	Market         string
	NoiseMin       float64 // 비율(0.025 = 2.5%)
	NoiseMax       float64
	NoiseWindow    int
	MAWindow       int
	K              float64
	RankDepth      int
	ActiveCount    int
	MinAffordable  int
	EvalPerMin     int
	StartDelay     time.Duration
	RefreshEvery   time.Duration
	KeepRank       int
	LazyExpand     bool // false면 첫 갱신의 랭킹 상위 종목으로 후보를 고정
	Seed           float64
	CommissionRate float64
}

func DefaultConfig(market string) Config {
	return Config{
		Market: market, NoiseMin: 0.025, NoiseMax: 0.06, NoiseWindow: 20, MAWindow: 5, K: 0.5,
		RankDepth: 30, ActiveCount: 10, MinAffordable: 2, EvalPerMin: 3,
		StartDelay: 5 * time.Minute, RefreshEvery: time.Minute, KeepRank: 0, LazyExpand: true,
	}
}

// Entry는 종목 하나의 하루치 평가 결과입니다(탈락 포함). JSON으로 저장·복구됩니다.

type Entry struct {
	Symbol     string            `json:"symbol"`
	Origin     string            `json:"origin"` // "prewarm" | "intraday"
	Passed     bool              `json:"passed"` // 1~3단계 통과
	Reason     string            `json:"reason,omitempty"`
	NoiseRatio float64           `json:"noise_ratio"`
	Prev       strategy.DailyBar `json:"prev"`
	Closes     []float64         `json:"closes,omitempty"` // 어제까지 MAWindow개, 최신순
	HasOpen    bool              `json:"has_open"`
	Open       float64           `json:"open"`
	Target     float64           `json:"target"`
	TrendOK    bool              `json:"trend_ok"`
}

// Setup은 시뮬레이터용 Setup입니다. HasOpen이 아니면 목표가가 0이므로 호출자가 걸러야 합니다.
func (e Entry) Setup() simulator.Setup {
	return simulator.Setup{Symbol: e.Symbol, TargetPrice: e.Target, TrendOK: e.TrendOK}
}

// BarsFromCandles는 일봉 캔들을 DailyBar로 바꿉니다(입력 순서 유지).
// 날짜나 가격을 해석할 수 없거나 가격이 0 이하인 봉은 버립니다.
func BarsFromCandles(candles []tossapi.Candle) []strategy.DailyBar {
	bars := make([]strategy.DailyBar, 0, len(candles))
	for _, c := range candles {
		if len(c.Timestamp) < len(dateLayout) {
			continue
		}
		open, e1 := strconv.ParseFloat(c.OpenPrice, 64)
		high, e2 := strconv.ParseFloat(c.HighPrice, 64)
		low, e3 := strconv.ParseFloat(c.LowPrice, 64)
		closePrice, e4 := strconv.ParseFloat(c.ClosePrice, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || open <= 0 || high <= 0 || low <= 0 || closePrice <= 0 {
			continue
		}
		bars = append(bars, strategy.DailyBar{Date: c.Timestamp[:len(dateLayout)], Open: open, High: high, Low: low, Close: closePrice})
	}
	return bars
}

// NoiseRatio는 최신순 prior의 앞 window개에 대한 평균 (고가-저가)/시가입니다.
func NoiseRatio(prior []strategy.DailyBar, window int) (float64, bool) {
	if window <= 0 || len(prior) < window {
		return 0, false
	}
	sum := 0.0
	for _, b := range prior[:window] {
		sum += (b.High - b.Low) / b.Open
	}
	return sum / float64(window), true
}

```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/screener; go vet ./internal/screener && go test ./internal/screener`
Expected: gofmt 출력 없음(있으면 `gofmt -w`), PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/screener
git commit -m "Add screener package skeleton: Config, Entry, daily-bar conversion, noise ratio"
```

---

### Task 3: `Evaluator` 정적 평가 (1~3단계), 캐시, 분당 예산

**Files:**
- Create: `internal/screener/evaluator.go`
- Test: `internal/screener/evaluator_test.go`

**Interfaces:**
- Consumes: Task 2의 `Source`, `Config`, `Entry`, `BarsFromCandles`, `NoiseRatio`, 테스트 헬퍼(`fakeSource`, `dailyCandles`, `testConfig`, `testSessionStart`).
- Produces:
  - `func NewEvaluator(src Source, cfg Config, sessionStart time.Time) *Evaluator`
  - `func (e *Evaluator) Static(ctx context.Context, symbol, origin string, now time.Time, limited bool) (Entry, bool, error)` — 캐시에 있으면 `(entry, true, nil)`. `limited`이고 이번 1분 예산이 소진됐으면 `(Entry{}, false, nil)`. API 실패는 `(Entry{}, false, err)`이며 **캐시하지 않는다**(다음에 재시도). 데이터상의 탈락(레버리지, 정리매매, 거래정지, 일봉 부족, 노이즈 구간 밖, 유의사항, 종목 정보 없음)은 `Passed=false, Reason`을 채워 캐시한다.
  - `func (e *Evaluator) Cached(symbol string) (Entry, bool)`
  - `func (e *Evaluator) Seed(entries []Entry)` — 콜백 없이 캐시에 채움(복구용)
  - `func (e *Evaluator) Entries() []Entry` — 종목명 오름차순
  - 필드 `OnEvaluated func(Entry)` — 새로 평가·갱신된 항목마다 호출(로그, 저장용)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/screener/evaluator_test.go`:

```go
package screener

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func newTestEvaluator(f *fakeSource) *Evaluator {
	return NewEvaluator(f, testConfig(), testSessionStart())
}

func passingSource(symbols ...string) *fakeSource {
	f := newFakeSource()
	for _, s := range symbols {
		f.addStock(s)
		f.daily[s] = dailyCandles("2026-10-01", 22, true)
	}
	return f
}

func TestStaticPassesAndUsesOnlyPriorBars(t *testing.T) {
	f := passingSource("A")
	ev := newTestEvaluator(f)
	e, ok, err := ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	if err != nil || !ok {
		t.Fatalf("Static = %v %v", ok, err)
	}
	if !e.Passed || e.Origin != "prewarm" {
		t.Fatalf("entry = %+v", e)
	}
	if e.NoiseRatio < 0.0399 || e.NoiseRatio > 0.0401 {
		t.Errorf("노이즈 = %v, 기대 0.04 (오늘 형성 중인 봉이 섞이면 달라집니다)", e.NoiseRatio)
	}
	if e.Prev.Date != "2026-09-30" || e.Prev.Close != 103 || len(e.Closes) != 5 || e.Closes[0] != 103 {
		t.Errorf("전일 봉/종가: %+v %v", e.Prev, e.Closes)
	}
	if e.HasOpen {
		t.Error("정적 평가 단계에서 시가가 확정되었습니다")
	}
}

func TestStaticRejections(t *testing.T) {
	f := passingSource("OK")

	f.addStock("LEV")
	f.stocks["LEV"] = tossapi.Stock{Symbol: "LEV", Raw: json.RawMessage(`{"leverageFactor":"3"}`)}
	f.daily["LEV"] = dailyCandles("2026-10-01", 22, true)

	f.stocks["LIQ"] = tossapi.Stock{Symbol: "LIQ", Raw: json.RawMessage(`{}`), KoreanMarketDetail: &tossapi.KoreanMarketDetail{LiquidationTrading: true}}
	f.stocks["SUS"] = tossapi.Stock{Symbol: "SUS", Raw: json.RawMessage(`{}`), KoreanMarketDetail: &tossapi.KoreanMarketDetail{KrxTradingSuspended: true}}

	f.addStock("SHORT")
	f.daily["SHORT"] = dailyCandles("2026-10-01", 10, true)

	f.addStock("QUIET")
	quiet := dailyCandles("2026-10-01", 22, true)
	for i := range quiet[1:] {
		quiet[i+1].HighPrice = "101" // 노이즈 1%
	}
	f.daily["QUIET"] = quiet

	f.addStock("WILD")
	wild := dailyCandles("2026-10-01", 22, true)
	for i := range wild[1:] {
		wild[i+1].HighPrice = "110" // 노이즈 10%
	}
	f.daily["WILD"] = wild

	f.addStock("WARN")
	f.daily["WARN"] = dailyCandles("2026-10-01", 22, true)
	f.warnings["WARN"] = []json.RawMessage{json.RawMessage(`{"type":"투자경고"}`)}

	cases := map[string]string{
		"LEV": "레버리지", "LIQ": "정리매매", "SUS": "거래정지", "SHORT": "일봉 부족",
		"QUIET": "노이즈", "WILD": "노이즈", "WARN": "유의사항", "NOSTOCK": "종목 정보 없음",
	}
	ev := newTestEvaluator(f)
	for symbol, wantReason := range cases {
		e, ok, err := ev.Static(context.Background(), symbol, "intraday", time.Now(), false)
		if err != nil || !ok {
			t.Fatalf("%s: Static = %v %v", symbol, ok, err)
		}
		if e.Passed || !strings.Contains(e.Reason, wantReason) {
			t.Errorf("%s: %+v, 기대 사유 %q", symbol, e, wantReason)
		}
	}
	if f.calls["Warnings"] != 1 {
		t.Errorf("유의사항 조회는 노이즈까지 통과한 종목에만: %d회", f.calls["Warnings"])
	}
}

func TestStaticCachesIncludingRejections(t *testing.T) {
	f := passingSource("A")
	f.stocks["B"] = tossapi.Stock{Symbol: "B", Raw: json.RawMessage(`{"leverageFactor":"3"}`)}
	ev := newTestEvaluator(f)
	for i := 0; i < 3; i++ {
		ev.Static(context.Background(), "A", "intraday", time.Now(), false)
		ev.Static(context.Background(), "B", "intraday", time.Now(), false)
	}
	if f.calls["Stocks"] != 2 {
		t.Errorf("Stocks 호출 %d회, 기대 2회(탈락도 캐시)", f.calls["Stocks"])
	}
}

func TestStaticAPIErrorIsNotCached(t *testing.T) {
	f := passingSource("A")
	f.errOn["Daily:A"] = errors.New("boom")
	ev := newTestEvaluator(f)
	if _, ok, err := ev.Static(context.Background(), "A", "intraday", time.Now(), false); ok || err == nil {
		t.Fatalf("오류가 전달되지 않았습니다: %v %v", ok, err)
	}
	if _, cached := ev.Cached("A"); cached {
		t.Fatal("API 오류가 캐시되었습니다")
	}
	delete(f.errOn, "Daily:A")
	if e, ok, err := ev.Static(context.Background(), "A", "intraday", time.Now(), false); err != nil || !ok || !e.Passed {
		t.Fatalf("재시도 실패: %+v %v %v", e, ok, err)
	}
}

func TestStaticPerMinuteBudget(t *testing.T) {
	f := passingSource("A", "B", "C")
	cfg := testConfig()
	cfg.EvalPerMin = 2
	ev := NewEvaluator(f, cfg, testSessionStart())
	now := time.Date(2026, 10, 1, 9, 5, 0, 0, kst)
	for _, s := range []string{"A", "B"} {
		if _, ok, _ := ev.Static(context.Background(), s, "intraday", now, true); !ok {
			t.Fatalf("%s: 예산 안인데 평가되지 않았습니다", s)
		}
	}
	if _, ok, err := ev.Static(context.Background(), "C", "intraday", now.Add(10*time.Second), true); ok || err != nil {
		t.Fatalf("예산 소진 후: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := ev.Static(context.Background(), "A", "intraday", now.Add(10*time.Second), true); !ok {
		t.Error("캐시된 종목은 예산과 무관하게 반환되어야 합니다")
	}
	if _, ok, _ := ev.Static(context.Background(), "C", "intraday", now.Add(61*time.Second), true); !ok {
		t.Error("1분이 지나면 예산이 복구되어야 합니다")
	}
	if _, ok, _ := ev.Static(context.Background(), "C", "prewarm", now, false); !ok {
		t.Error("limited=false는 예산을 쓰지 않습니다")
	}
}

func TestOnEvaluatedAndSeed(t *testing.T) {
	f := passingSource("A")
	ev := newTestEvaluator(f)
	var seen []string
	ev.OnEvaluated = func(e Entry) { seen = append(seen, e.Symbol) }
	ev.Seed([]Entry{{Symbol: "S", Passed: true}})
	ev.Static(context.Background(), "A", "intraday", time.Now(), false)
	ev.Static(context.Background(), "A", "intraday", time.Now(), false)
	ev.Static(context.Background(), "S", "intraday", time.Now(), false)
	if len(seen) != 1 || seen[0] != "A" {
		t.Errorf("콜백 호출: %v (Seed와 캐시 적중은 호출하지 않아야 합니다)", seen)
	}
	got := ev.Entries()
	if len(got) != 2 || got[0].Symbol != "A" || got[1].Symbol != "S" {
		t.Errorf("Entries = %+v", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/screener`
Expected: FAIL (`NewEvaluator` 미정의)

- [ ] **Step 3: 구현**

`internal/screener/evaluator.go`:

```go
package screener

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tibetkowon/toss-trader/internal/strategy"
)

// Evaluator는 종목 평가와 하루 캐시를 맡습니다. 동시 사용은 지원하지 않습니다.
type Evaluator struct {
	src          Source
	cfg          Config
	sessionStart time.Time
	cache        map[string]Entry
	windowStart  time.Time
	windowUsed   int

	OnEvaluated func(Entry)
}

func NewEvaluator(src Source, cfg Config, sessionStart time.Time) *Evaluator {
	return &Evaluator{src: src, cfg: cfg, sessionStart: sessionStart, cache: make(map[string]Entry)}
}

func (e *Evaluator) Cached(symbol string) (Entry, bool) {
	entry, ok := e.cache[symbol]
	return entry, ok
}

func (e *Evaluator) Seed(entries []Entry) {
	for _, entry := range entries {
		e.cache[entry.Symbol] = entry
	}
}

func (e *Evaluator) Entries() []Entry {
	out := make([]Entry, 0, len(e.cache))
	for _, entry := range e.cache {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

func (e *Evaluator) store(entry Entry) {
	e.cache[entry.Symbol] = entry
	if e.OnEvaluated != nil {
		e.OnEvaluated(entry)
	}
}

func (e *Evaluator) takeBudget(now time.Time) bool {
	if e.cfg.EvalPerMin <= 0 {
		return true
	}
	if e.windowStart.IsZero() || now.Sub(e.windowStart) >= time.Minute {
		e.windowStart = now
		e.windowUsed = 0
	}
	if e.windowUsed >= e.cfg.EvalPerMin {
		return false
	}
	e.windowUsed++
	return true
}

// Static은 설계 3절의 1~3단계(속성, 노이즈, 유의사항)를 평가합니다.
func (e *Evaluator) Static(ctx context.Context, symbol, origin string, now time.Time, limited bool) (Entry, bool, error) {
	if entry, ok := e.cache[symbol]; ok {
		return entry, true, nil
	}
	if limited && !e.takeBudget(now) {
		return Entry{}, false, nil
	}
	entry, err := e.evaluate(ctx, symbol, origin)
	if err != nil {
		return Entry{}, false, err
	}
	e.store(entry)
	return entry, true, nil
}

func reject(entry Entry, format string, args ...any) Entry {
	entry.Passed = false
	entry.Reason = fmt.Sprintf(format, args...)
	return entry
}

func (e *Evaluator) evaluate(ctx context.Context, symbol, origin string) (Entry, error) {
	entry := Entry{Symbol: symbol, Origin: origin}

	stocks, err := e.src.Stocks(ctx, symbol)
	if err != nil {
		return entry, fmt.Errorf("%s 종목 정보 조회: %w", symbol, err)
	}
	if len(stocks) == 0 {
		return reject(entry, "종목 정보 없음"), nil
	}
	stock := stocks[0]
	switch {
	case stock.IsLeveraged():
		return reject(entry, "레버리지/인버스"), nil
	case stock.KoreanMarketDetail != nil && stock.KoreanMarketDetail.LiquidationTrading:
		return reject(entry, "정리매매"), nil
	case stock.KoreanMarketDetail != nil && stock.KoreanMarketDetail.KrxTradingSuspended:
		return reject(entry, "거래정지"), nil
	}

	need := max(e.cfg.NoiseWindow, e.cfg.MAWindow)
	candles, _, err := e.src.Candles(ctx, symbol, "1d", need+2, "")
	if err != nil {
		return entry, fmt.Errorf("%s 일봉 조회: %w", symbol, err)
	}
	sessionDate := e.sessionStart.Format(dateLayout)
	_, prior := strategy.SplitBars(BarsFromCandles(candles), sessionDate)
	if len(prior) < need {
		return reject(entry, "일봉 부족(어제 이전 %d개)", len(prior)), nil
	}
	ratio, _ := NoiseRatio(prior, e.cfg.NoiseWindow)
	entry.NoiseRatio = ratio
	if ratio < e.cfg.NoiseMin || ratio > e.cfg.NoiseMax {
		return reject(entry, "노이즈 %.2f%%가 구간(%.1f~%.1f%%) 밖", ratio*100, e.cfg.NoiseMin*100, e.cfg.NoiseMax*100), nil
	}

	warnings, err := e.src.StockWarnings(ctx, symbol)
	if err != nil {
		return entry, fmt.Errorf("%s 유의사항 조회: %w", symbol, err)
	}
	if len(warnings) > 0 {
		return reject(entry, "유의사항 %d건", len(warnings)), nil
	}

	entry.Passed = true
	entry.Prev = prior[0]
	entry.Closes = make([]float64, e.cfg.MAWindow)
	for i := range entry.Closes {
		entry.Closes[i] = prior[i].Close
	}
	return entry, nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/screener; go test ./internal/screener`
Expected: gofmt 출력 없음(있으면 `gofmt -w`), PASS

- [ ] **Step 5: 커밋**

```bash
gofmt -w internal/screener
git add internal/screener
git commit -m "Add screener package: types, static evaluator, per-minute budget"
```

---

### Task 4: `Evaluator.Complete` — 오늘 시가로 목표가·추세 확정 (4단계)

**Files:**
- Modify: `internal/screener/evaluator.go`
- Test: `internal/screener/evaluator_test.go`

**Interfaces:**
- Consumes: Task 3의 `Evaluator`, `Entry`.
- Produces: `func (e *Evaluator) Complete(ctx context.Context, symbol string) (Entry, error)` — 캐시에 통과한 항목이 없으면 오류. 이미 `HasOpen`이면 그대로 반환(호출 없음). 정규장 시작 이후 가장 이른 1분봉의 시가를 쓴다(페이지네이션, 최대 4쪽). 시가를 못 찾으면 `HasOpen=false`인 항목과 `nil`을 반환하고 캐시하지 않는다(다음 갱신에 재시도). 성공하면 `Open`, `Target`, `TrendOK`를 채워 캐시·콜백.

- [ ] **Step 1: 실패하는 테스트 작성**

`evaluator_test.go` 끝에 추가:

```go
func TestCompleteComputesTargetFromRegularSessionOpen(t *testing.T) {
	f := passingSource("A")
	start := testSessionStart()
	f.minute["A"] = []tossapi.Candle{
		minuteCandle(start.Add(2*time.Minute), "120"),
		minuteCandle(start.Add(time.Minute), "111"),
		minuteCandle(start, "110"),
		minuteCandle(start.Add(-time.Minute), "90"), // 장전(NXT) 봉은 무시
	}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)

	e, err := ev.Complete(context.Background(), "A")
	if err != nil || !e.HasOpen {
		t.Fatalf("Complete = %+v %v", e, err)
	}
	// 시가 110 + (104-100)*0.5 = 112, 전일 종가 103 > MA5 100.6
	if e.Open != 110 || e.Target != 112 || !e.TrendOK {
		t.Errorf("open/target/trend = %v %v %v", e.Open, e.Target, e.TrendOK)
	}
	if again, _ := ev.Complete(context.Background(), "A"); !again.HasOpen || f.calls["Minute"] != 1 {
		t.Errorf("확정된 항목은 다시 조회하지 않아야 합니다: 1분봉 호출 %d회", f.calls["Minute"])
	}
}

func TestCompleteFallsBackToEarliestBarAfterStart(t *testing.T) {
	f := passingSource("A")
	start := testSessionStart()
	f.minute["A"] = []tossapi.Candle{
		minuteCandle(start.Add(3*time.Minute), "130"),
		minuteCandle(start.Add(time.Minute), "115"), // 09:00 봉이 없는 저유동 종목
	}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	e, err := ev.Complete(context.Background(), "A")
	if err != nil || !e.HasOpen || e.Open != 115 {
		t.Fatalf("Complete = %+v %v", e, err)
	}
}

func TestCompleteNoBarsYetIsRetriable(t *testing.T) {
	f := passingSource("A")
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart().Add(-time.Minute), "90")}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	e, err := ev.Complete(context.Background(), "A")
	if err != nil || e.HasOpen {
		t.Fatalf("시가가 없는데 확정되었습니다: %+v %v", e, err)
	}
	if cached, _ := ev.Cached("A"); cached.HasOpen {
		t.Error("미확정 상태가 캐시에 확정으로 남았습니다")
	}
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart(), "100")}
	if e, err := ev.Complete(context.Background(), "A"); err != nil || !e.HasOpen {
		t.Fatalf("재시도 실패: %+v %v", e, err)
	}
}

func TestCompleteRequiresPassedEntry(t *testing.T) {
	f := newFakeSource()
	ev := newTestEvaluator(f)
	if _, err := ev.Complete(context.Background(), "NOPE"); err == nil {
		t.Error("평가되지 않은 종목에 오류가 없습니다")
	}
	ev.Seed([]Entry{{Symbol: "R", Passed: false, Reason: "x"}})
	if _, err := ev.Complete(context.Background(), "R"); err == nil {
		t.Error("탈락 종목에 오류가 없습니다")
	}
}

func TestCompleteMinuteAPIErrorPropagates(t *testing.T) {
	f := passingSource("A")
	f.errOn["Minute:A"] = errors.New("429")
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	if _, err := ev.Complete(context.Background(), "A"); err == nil {
		t.Fatal("API 오류가 전달되지 않았습니다")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/screener -run TestComplete`
Expected: FAIL (`Complete` 미정의)

- [ ] **Step 3: 구현**

`evaluator.go`에 추가 (`time`, `strconv` import 필요):

```go
// Complete는 통과한 종목에 오늘 시가를 붙여 목표가와 추세 결과를 확정합니다(4단계).
// 시가를 아직 못 찾으면 HasOpen=false인 항목을 오류 없이 돌려주며 캐시하지 않습니다.
func (e *Evaluator) Complete(ctx context.Context, symbol string) (Entry, error) {
	entry, ok := e.cache[symbol]
	if !ok || !entry.Passed {
		return Entry{}, fmt.Errorf("%s: 정적 평가를 통과한 항목이 없습니다", symbol)
	}
	if entry.HasOpen {
		return entry, nil
	}
	open, found, err := e.regularOpen(ctx, symbol)
	if err != nil {
		return entry, fmt.Errorf("%s 1분봉 조회: %w", symbol, err)
	}
	if !found {
		return entry, nil
	}
	target, trendOK, err := strategy.ComputeDaySetup(open, entry.Prev, entry.Closes, e.cfg.K)
	if err != nil {
		return entry, fmt.Errorf("%s 셋업 계산: %w", symbol, err)
	}
	entry.HasOpen, entry.Open, entry.Target, entry.TrendOK = true, open, target, trendOK
	e.store(entry)
	return entry, nil
}

// regularOpen은 정규장 시작 이후 가장 이른 1분봉의 시가를 찾습니다. 장전(NXT 등) 봉은 무시하고,
// 정규장 첫 봉이 없는 저유동 종목은 그 이후 가장 이른 봉으로 대신합니다.
func (e *Evaluator) regularOpen(ctx context.Context, symbol string) (float64, bool, error) {
	before := ""
	var bestTime time.Time
	var bestOpen float64
	found := false
	for page := 0; page < 4; page++ {
		candles, next, err := e.src.Candles(ctx, symbol, "1m", 200, before)
		if err != nil {
			return 0, false, err
		}
		reachedStart := false
		for _, c := range candles {
			ts, err := time.Parse(time.RFC3339, c.Timestamp)
			if err != nil {
				continue
			}
			if ts.Before(e.sessionStart) {
				reachedStart = true
				continue
			}
			open, err := strconv.ParseFloat(c.OpenPrice, 64)
			if err != nil || open <= 0 {
				continue
			}
			if !found || ts.Before(bestTime) {
				bestTime, bestOpen, found = ts, open, true
			}
		}
		if reachedStart || next == "" {
			break
		}
		before = next
	}
	return bestOpen, found, nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/screener; go test ./internal/screener`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/screener
git commit -m "Add Evaluator.Complete: target and trend from the regular session's first bar"
```

---

### Task 5: `SelectActive` 순수 함수 (상위 N, 히스테리시스, 최소 살 수 있는 종목)

**Files:**
- Create: `internal/screener/select.go`
- Test: `internal/screener/select_test.go`

**Interfaces:**
- Produces:
  - `type Ranked struct { Symbol string; Rank int; Price float64 }` — 평가를 통과하고 시가까지 확정된 종목의 당일 랭킹 항목. `Price`는 랭킹 응답의 현재가(해석 실패 시 0).
  - `func SelectActive(passing []Ranked, prev []string, activeCount, keepRank, minAffordable int, maxPrice float64) []string`
    - 결과는 랭킹 오름차순, 길이는 `activeCount` 이하.
    - `keepRank > 0`이면 직전 활성 종목 중 랭킹이 `keepRank` 이내인 종목을 먼저 유지한다.
    - `minAffordable > 0 && maxPrice > 0`이면 `0 < Price <= maxPrice`인 종목이 결과에 `minAffordable`개 이상 있도록, 결과에서 랭킹이 가장 낮은 "못 사는" 종목을 후보 밖의 살 수 있는 종목으로 교체한다(교체할 살 수 있는 종목이 없으면 그대로).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/screener/select_test.go`:

```go
package screener

import (
	"reflect"
	"testing"
)

func ranked(pairs ...any) []Ranked {
	var out []Ranked
	for i := 0; i < len(pairs); i += 3 {
		out = append(out, Ranked{Symbol: pairs[i].(string), Rank: pairs[i+1].(int), Price: pairs[i+2].(float64)})
	}
	return out
}

func TestSelectActiveTopNByRank(t *testing.T) {
	passing := ranked("C", 3, 10.0, "A", 1, 10.0, "B", 2, 10.0, "D", 4, 10.0)
	got := SelectActive(passing, nil, 2, 0, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSelectActiveFewerThanCount(t *testing.T) {
	if got := SelectActive(ranked("A", 1, 10.0), nil, 10, 0, 0, 0); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("got %v", got)
	}
	if got := SelectActive(nil, []string{"X"}, 10, 20, 2, 100); len(got) != 0 {
		t.Errorf("빈 입력에서 %v", got)
	}
}

func TestSelectActiveHysteresisKeepsPrevWithinKeepRank(t *testing.T) {
	passing := ranked("A", 1, 10.0, "B", 2, 10.0, "C", 3, 10.0, "D", 15, 10.0)
	// 직전 활성이 D였고 D는 keepRank(20) 이내 -> 밀려나지 않고 유지, 남는 한 자리는 랭킹 1위
	got := SelectActive(passing, []string{"D"}, 2, 20, 0, 0)
	if want := []string{"A", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// keepRank 밖으로 밀리면 유지하지 않는다
	got = SelectActive(passing, []string{"D"}, 2, 10, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// keepRank 0이면 꺼짐
	got = SelectActive(passing, []string{"D"}, 2, 0, 0, 0)
	if want := []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSelectActiveMinAffordableSwapsLowestUnaffordable(t *testing.T) {
	// maxPrice 100: A,B는 못 산다(비싸다). C는 살 수 있다.
	passing := ranked("A", 1, 500.0, "B", 2, 300.0, "C", 3, 50.0, "D", 4, 60.0)
	got := SelectActive(passing, nil, 2, 0, 1, 100)
	if want := []string{"A", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("최소 1개: got %v, want %v", got, want)
	}
	got = SelectActive(passing, nil, 2, 0, 2, 100)
	if want := []string{"C", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("최소 2개: got %v, want %v", got, want)
	}
}

func TestSelectActiveMinAffordableNoCandidatesOrRuleOff(t *testing.T) {
	passing := ranked("A", 1, 500.0, "B", 2, 300.0)
	if got := SelectActive(passing, nil, 2, 0, 2, 100); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("살 수 있는 종목이 없으면 그대로: %v", got)
	}
	if got := SelectActive(ranked("A", 1, 500.0, "C", 2, 50.0), nil, 1, 0, 1, 0); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("maxPrice 0이면 규칙 꺼짐: %v", got)
	}
}

func TestSelectActiveZeroPriceIsUnaffordable(t *testing.T) {
	passing := ranked("A", 1, 0.0, "B", 2, 50.0)
	got := SelectActive(passing, nil, 1, 0, 1, 100)
	if !reflect.DeepEqual(got, []string{"B"}) {
		t.Errorf("가격을 모르는 종목은 살 수 있다고 세지 않습니다: %v", got)
	}
}

func TestSelectActiveDoesNotMutateInput(t *testing.T) {
	passing := ranked("C", 3, 1.0, "A", 1, 1.0)
	SelectActive(passing, nil, 2, 0, 0, 0)
	if passing[0].Symbol != "C" {
		t.Error("입력 슬라이스 순서가 바뀌었습니다")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/screener -run TestSelectActive`
Expected: FAIL (`SelectActive` 미정의)

- [ ] **Step 3: 구현**

`internal/screener/select.go`:

```go
package screener

import "sort"

type Ranked struct {
	Symbol string
	Rank   int
	Price  float64
}

func affordable(r Ranked, maxPrice float64) bool {
	return maxPrice > 0 && r.Price > 0 && r.Price <= maxPrice
}

func SelectActive(passing []Ranked, prev []string, activeCount, keepRank, minAffordable int, maxPrice float64) []string {
	sorted := append([]Ranked(nil), passing...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rank < sorted[j].Rank })

	var chosen []Ranked
	in := make(map[string]bool)
	add := func(r Ranked) {
		chosen = append(chosen, r)
		in[r.Symbol] = true
	}

	if keepRank > 0 {
		wasActive := make(map[string]bool, len(prev))
		for _, s := range prev {
			wasActive[s] = true
		}
		for _, r := range sorted {
			if len(chosen) >= activeCount {
				break
			}
			if wasActive[r.Symbol] && r.Rank <= keepRank {
				add(r)
			}
		}
	}
	for _, r := range sorted {
		if len(chosen) >= activeCount {
			break
		}
		if !in[r.Symbol] {
			add(r)
		}
	}

	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Rank < chosen[j].Rank })

	if minAffordable > 0 && maxPrice > 0 {
		count := 0
		for _, r := range chosen {
			if affordable(r, maxPrice) {
				count++
			}
		}
		for _, r := range sorted {
			if count >= minAffordable {
				break
			}
			if in[r.Symbol] || !affordable(r, maxPrice) {
				continue
			}
			victim := -1
			for i := len(chosen) - 1; i >= 0; i-- { // 랭킹이 가장 낮은 못 사는 종목
				if !affordable(chosen[i], maxPrice) {
					victim = i
					break
				}
			}
			if victim < 0 {
				break
			}
			delete(in, chosen[victim].Symbol)
			chosen[victim] = r
			in[r.Symbol] = true
			count++
		}
	}

	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Rank < chosen[j].Rank })
	symbols := make([]string, len(chosen))
	for i, r := range chosen {
		symbols[i] = r.Symbol
	}
	return symbols
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/screener; go test ./internal/screener`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/screener
git commit -m "Add SelectActive: top-N by live rank with hysteresis and affordability rule"
```

---

### Task 6: `Gate` — 사전 평가, 장중 갱신, 실패 시 대체

**Files:**
- Create: `internal/screener/gate.go`
- Test: `internal/screener/gate_test.go`

**Interfaces:**
- Consumes: Task 3~5의 `Evaluator`(`Static`, `Complete`, `Cached`), `SelectActive`, `Ranked`, `Config`, `Source`.
- Produces:
  - `func NewGate(cfg Config, src Source, ev *Evaluator, sessionStart time.Time) *Gate`
  - `func (g *Gate) SetSeed(seed float64)` — 오늘 시드 확정 후 호출(살 수 있는 종목 판정용)
  - `func (g *Gate) PreWarm(ctx context.Context, deadline time.Time) PreWarmResult` — 장 시작 전, 어제 마감 기준 `Rankings(1d)` 상위 `RankDepth`개를 정적 평가(예산 없음, 마감 시각 초과 시 중단).
  - `func (g *Gate) Refresh(ctx context.Context, now time.Time) Update` — 개장 후 `StartDelay` 전이거나 직전 갱신 후 `RefreshEvery` 미만이면 `Update{}`(Ran=false). 그 외에는 랭킹 조회 → 미평가 종목 평가(분당 예산) → 통과 종목 시가 확정 → `SelectActive`.
  - `func (g *Gate) Active() []string` (랭킹 순, 복사본), `func (g *Gate) Rank(symbol string) int`, `func (g *Gate) Setup(symbol string) (simulator.Setup, bool)` (통과하고 시가가 확정된 캐시 항목만), `func (g *Gate) Evaluator() *Evaluator`
  - `type Update struct { Ran bool; Added, Removed []string; Skipped int; UsedFallback bool; Errs []error }`
  - `type PreWarmResult struct { Ranked, Evaluated, Passed int; Top []string; TimedOut bool; Err error; SymbolErrs []error }`
- 동작 규칙: 랭킹 조회가 실패하거나 빈 응답이면 직전 활성 집합을 유지한다. 한 번도 랭킹을 받은 적이 없고 활성 집합이 비어 있으면 사전 평가 통과 종목(전일 랭킹 순)으로 대체한다(`UsedFallback`). `LazyExpand=false`면 첫 성공 갱신의 상위 종목으로 후보를 고정하고 이후 새 종목은 평가하지 않는다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/screener/gate_test.go`:

```go
package screener

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

// gateFixture는 모두 평가를 통과하는 종목들과, 시가 110(목표가 112)이 확정 가능한 1분봉을 만듭니다.
func gateFixture(symbols ...string) *fakeSource {
	f := passingSource(symbols...)
	for _, s := range symbols {
		f.minute[s] = []tossapi.Candle{minuteCandle(testSessionStart(), "110")}
	}
	return f
}

func setRanking(f *fakeSource, symbols ...string) {
	f.rankings = nil
	for i, s := range symbols {
		f.rankings = append(f.rankings, rankingItem(i+1, s, "50"))
	}
}

func newTestGate(f *fakeSource, mutate func(*Config)) *Gate {
	cfg := testConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	return NewGate(cfg, f, NewEvaluator(f, cfg, testSessionStart()), testSessionStart())
}

var afterDelay = testSessionStart().Add(6 * time.Minute)

func TestRefreshWaitsForStartDelay(t *testing.T) {
	f := gateFixture("A", "B")
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	if up := g.Refresh(context.Background(), testSessionStart().Add(4*time.Minute)); up.Ran {
		t.Fatal("지연 전에 갱신했습니다")
	}
	if f.calls["Rankings"] != 0 || len(g.Active()) != 0 {
		t.Fatalf("지연 전 호출/활성: %d %v", f.calls["Rankings"], g.Active())
	}
}

func TestRefreshSelectsTopActiveAndSetup(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, nil)
	up := g.Refresh(context.Background(), afterDelay)
	if !up.Ran || !reflect.DeepEqual(up.Added, []string{"A", "B"}) || len(up.Removed) != 0 {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) || g.Rank("A") != 1 || g.Rank("B") != 2 {
		t.Fatalf("active=%v ranks=%d,%d", g.Active(), g.Rank("A"), g.Rank("B"))
	}
	setup, ok := g.Setup("A")
	if !ok || setup.TargetPrice != 112 || !setup.TrendOK {
		t.Errorf("Setup = %+v %v", setup, ok)
	}
	if _, ok := g.Setup("ZZZ"); ok {
		t.Error("모르는 종목의 Setup이 있습니다")
	}
}

func TestRefreshIntervalAndRankChange(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	setRanking(f, "C", "A", "B")
	if up := g.Refresh(context.Background(), afterDelay.Add(30*time.Second)); up.Ran {
		t.Fatal("RefreshEvery 안에 다시 갱신했습니다")
	}
	up := g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	if !up.Ran || !reflect.DeepEqual(up.Added, []string{"C"}) || !reflect.DeepEqual(up.Removed, []string{"B"}) {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"C", "A"}) {
		t.Errorf("active = %v", g.Active())
	}
}

func TestRefreshSkipsRejectedSymbols(t *testing.T) {
	f := gateFixture("B", "C")
	f.addStock("LEV")
	f.stocks["LEV"] = tossapi.Stock{Symbol: "LEV", Raw: []byte(`{"leverageFactor":"3"}`)}
	setRanking(f, "LEV", "B", "C")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"B", "C"}) {
		t.Fatalf("active = %v", g.Active())
	}
	if e, _ := g.Evaluator().Cached("LEV"); e.Passed || e.Reason == "" {
		t.Errorf("탈락 사유가 기록되지 않았습니다: %+v", e)
	}
}

func TestRefreshRankingFailureKeepsPrevious(t *testing.T) {
	f := gateFixture("A", "B")
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)

	f.rankingErr = errors.New("503")
	up := g.Refresh(context.Background(), afterDelay.Add(2*time.Minute))
	if !up.Ran || len(up.Errs) == 0 || up.UsedFallback {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("실패 시 직전 집합을 유지해야 합니다: %v", g.Active())
	}

	f.rankingErr = nil
	f.rankings = nil // 빈 응답도 실패로 취급
	up = g.Refresh(context.Background(), afterDelay.Add(4*time.Minute))
	if len(up.Errs) == 0 || !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("빈 랭킹: %+v %v", up, g.Active())
	}
}

func TestRefreshFallsBackToPrewarmedWhenNeverRanked(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C") // 전일 랭킹
	g := newTestGate(f, nil)
	res := g.PreWarm(context.Background(), time.Now().Add(time.Minute))
	if res.Err != nil || res.Passed != 3 || res.Evaluated != 3 || res.TimedOut || !reflect.DeepEqual(res.Top, []string{"A", "B", "C"}) {
		t.Fatalf("PreWarm = %+v", res)
	}
	f.rankingErr = errors.New("503")
	up := g.Refresh(context.Background(), afterDelay)
	if !up.UsedFallback || !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Fatalf("대체 집합: %+v %v", up, g.Active())
	}
}

func TestPreWarmDeadlineAndFailure(t *testing.T) {
	f := gateFixture("A")
	setRanking(f, "A")
	g := newTestGate(f, nil)
	res := g.PreWarm(context.Background(), time.Now().Add(-time.Second))
	if !res.TimedOut || res.Evaluated != 0 || f.calls["Stocks"] != 0 {
		t.Errorf("마감 초과 시 평가하지 않아야 합니다: %+v", res)
	}
	f.rankingErr = errors.New("503")
	if res := g.PreWarm(context.Background(), time.Now().Add(time.Minute)); res.Err == nil {
		t.Error("랭킹 오류가 전달되지 않았습니다")
	}
}

func TestRefreshBudgetSpreadsEvaluationsAcrossMinutes(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, func(c *Config) { c.EvalPerMin = 1; c.ActiveCount = 3 })
	up := g.Refresh(context.Background(), afterDelay)
	if up.Skipped != 2 || !reflect.DeepEqual(g.Active(), []string{"A"}) {
		t.Fatalf("첫 갱신: %+v %v", up, g.Active())
	}
	g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	g.Refresh(context.Background(), afterDelay.Add(122*time.Second))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B", "C"}) {
		t.Errorf("세 번째 갱신 후 active = %v", g.Active())
	}
}

func TestRefreshLazyExpandOffFreezesCandidates(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B")
	g := newTestGate(f, func(c *Config) { c.LazyExpand = false })
	g.Refresh(context.Background(), afterDelay)
	setRanking(f, "C", "A", "B") // C가 장중에 떠오름
	g.Refresh(context.Background(), afterDelay.Add(2*time.Minute))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("고정 후보 밖의 C가 편입되었습니다: %v", g.Active())
	}
	if _, cached := g.Evaluator().Cached("C"); cached {
		t.Error("고정 모드에서 새 종목을 평가했습니다")
	}
}

func TestRefreshSkipsSymbolWithoutOpenYet(t *testing.T) {
	f := gateFixture("A", "B")
	f.minute["A"] = nil // 시가를 아직 못 구함
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"B"}) {
		t.Fatalf("active = %v", g.Active())
	}
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart(), "110")}
	g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("시가 확정 후 편입되지 않았습니다: %v", g.Active())
	}
}

func TestRefreshMinAffordableUsesSeed(t *testing.T) {
	f := gateFixture("A", "B", "C")
	f.rankings = []tossapi.RankingItem{rankingItem(1, "A", "900000"), rankingItem(2, "B", "800000"), rankingItem(3, "C", "5000")}
	g := newTestGate(f, func(c *Config) { c.MinAffordable = 1 })
	g.SetSeed(100000)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"A", "C"}) {
		t.Errorf("살 수 있는 종목이 최소 1개 포함되어야 합니다: %v", g.Active())
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/screener -run 'TestRefresh|TestPreWarm'`
Expected: FAIL (`NewGate` 미정의)

- [ ] **Step 3: 구현**

`internal/screener/gate.go`:

```go
package screener

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

const rankingType = "MARKET_TRADING_AMOUNT"

type Gate struct {
	cfg          Config
	src          Source
	ev           *Evaluator
	sessionStart time.Time
	now          func() time.Time

	active      []string
	ranks       map[string]int
	prewarmed   []Ranked // 사전 평가를 통과한 종목, 전일 랭킹 순
	lastRefresh time.Time
	everRanked  bool
	frozen      map[string]bool
}

func NewGate(cfg Config, src Source, ev *Evaluator, sessionStart time.Time) *Gate {
	return &Gate{cfg: cfg, src: src, ev: ev, sessionStart: sessionStart, now: time.Now, ranks: map[string]int{}}
}

func (g *Gate) SetSeed(seed float64) { g.cfg.Seed = seed }

func (g *Gate) Evaluator() *Evaluator { return g.ev }

func (g *Gate) Active() []string { return append([]string(nil), g.active...) }

func (g *Gate) Rank(symbol string) int { return g.ranks[symbol] }

func (g *Gate) Setup(symbol string) (simulator.Setup, bool) {
	e, ok := g.ev.Cached(symbol)
	if !ok || !e.Passed || !e.HasOpen {
		return simulator.Setup{}, false
	}
	return e.Setup(), true
}

func (g *Gate) maxPrice() float64 {
	if g.cfg.Seed <= 0 {
		return 0
	}
	return g.cfg.Seed / (1 + g.cfg.CommissionRate)
}

type rankedItem struct {
	Symbol string
	Rank   int
	Price  float64
}

// normalizeRanking은 Rank 필드 기준으로 정렬하고(응답 순서를 믿지 않음), 빈/중복 심볼을 버리고,
// 상위 depth개로 자릅니다. Rank가 없으면(0 이하) 정렬 후 위치를 순위로 씁니다.
func normalizeRanking(items []tossapi.RankingItem, depth int) []rankedItem {
	sorted := append([]tossapi.RankingItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rank < sorted[j].Rank })
	seen := make(map[string]bool, len(sorted))
	var out []rankedItem
	for i, it := range sorted {
		if it.Symbol == "" || seen[it.Symbol] {
			continue
		}
		seen[it.Symbol] = true
		rank := it.Rank
		if rank <= 0 {
			rank = i + 1
		}
		price, err := strconv.ParseFloat(it.Price.LastPrice, 64)
		if err != nil || price < 0 {
			price = 0
		}
		out = append(out, rankedItem{Symbol: it.Symbol, Rank: rank, Price: price})
		if depth > 0 && len(out) >= depth {
			break
		}
	}
	return out
}

type PreWarmResult struct {
	Ranked, Evaluated, Passed int
	Top                       []string // 사전 랭킹 상위 5개(장 시작 전 랭킹 내용 확인용 로그)
	TimedOut                  bool
	Err                       error
	SymbolErrs                []error
}

// PreWarm은 장 시작 전에 어제 마감 기준 랭킹 상위 종목을 미리 정적 평가합니다(설계 3절).
func (g *Gate) PreWarm(ctx context.Context, deadline time.Time) PreWarmResult {
	var res PreWarmResult
	items, err := g.src.Rankings(ctx, rankingType, "1d", g.cfg.Market)
	if err != nil {
		res.Err = fmt.Errorf("사전 랭킹 조회: %w", err)
		return res
	}
	ranked := normalizeRanking(items, g.cfg.RankDepth)
	res.Ranked = len(ranked)
	for i, it := range ranked {
		if i < 5 {
			res.Top = append(res.Top, it.Symbol)
		}
	}
	for _, it := range ranked {
		if g.now().After(deadline) {
			res.TimedOut = true
			break
		}
		e, ok, err := g.ev.Static(ctx, it.Symbol, "prewarm", g.now(), false)
		if err != nil {
			res.SymbolErrs = append(res.SymbolErrs, err)
			continue
		}
		if !ok {
			continue
		}
		res.Evaluated++
		if e.Passed {
			res.Passed++
			g.prewarmed = append(g.prewarmed, Ranked{Symbol: it.Symbol, Rank: it.Rank, Price: it.Price})
		}
	}
	return res
}

type Update struct {
	Ran          bool
	Added        []string
	Removed      []string
	Skipped      int // 분당 예산 때문에 이번에 평가하지 못한 종목 수
	UsedFallback bool
	Errs         []error
}

// ready는 통과한 종목의 오늘 시가·목표가가 확정되었는지 보장합니다.
func (g *Gate) ready(ctx context.Context, e Entry) (bool, error) {
	if !e.Passed {
		return false, nil
	}
	if e.HasOpen {
		return true, nil
	}
	done, err := g.ev.Complete(ctx, e.Symbol)
	if err != nil {
		return false, err
	}
	return done.HasOpen, nil
}

func (g *Gate) Refresh(ctx context.Context, now time.Time) Update {
	if now.Before(g.sessionStart.Add(g.cfg.StartDelay)) {
		return Update{}
	}
	if !g.lastRefresh.IsZero() && now.Sub(g.lastRefresh) < g.cfg.RefreshEvery {
		return Update{}
	}
	g.lastRefresh = now
	up := Update{Ran: true}

	items, err := g.src.Rankings(ctx, rankingType, "1d", g.cfg.Market)
	if err == nil && len(items) == 0 {
		err = errors.New("랭킹 응답이 비었습니다")
	}
	if err != nil {
		up.Errs = append(up.Errs, fmt.Errorf("랭킹 조회: %w", err))
		if !g.everRanked && len(g.active) == 0 {
			up.UsedFallback = true
			var passing []Ranked
			for _, r := range g.prewarmed {
				e, ok := g.ev.Cached(r.Symbol)
				if !ok {
					continue
				}
				if isReady, err := g.ready(ctx, e); err != nil {
					up.Errs = append(up.Errs, err)
				} else if isReady {
					passing = append(passing, r)
				}
			}
			g.apply(&up, passing)
		}
		return up
	}
	g.everRanked = true

	ranked := normalizeRanking(items, g.cfg.RankDepth)
	if !g.cfg.LazyExpand && g.frozen == nil {
		g.frozen = make(map[string]bool, len(ranked))
		for _, it := range ranked {
			g.frozen[it.Symbol] = true
		}
	}
	var passing []Ranked
	for _, it := range ranked {
		if g.frozen != nil && !g.frozen[it.Symbol] {
			continue
		}
		e, ok, err := g.ev.Static(ctx, it.Symbol, "intraday", now, true)
		if err != nil {
			up.Errs = append(up.Errs, err)
			continue
		}
		if !ok {
			up.Skipped++
			continue
		}
		if isReady, err := g.ready(ctx, e); err != nil {
			up.Errs = append(up.Errs, err)
		} else if isReady {
			passing = append(passing, Ranked{Symbol: it.Symbol, Rank: it.Rank, Price: it.Price})
		}
	}
	g.apply(&up, passing)
	return up
}

func (g *Gate) apply(up *Update, passing []Ranked) {
	next := SelectActive(passing, g.active, g.cfg.ActiveCount, g.cfg.KeepRank, g.cfg.MinAffordable, g.maxPrice())
	up.Added, up.Removed = diffSets(g.active, next)
	g.active = next
	g.ranks = make(map[string]int, len(next))
	rankOf := make(map[string]int, len(passing))
	for _, r := range passing {
		rankOf[r.Symbol] = r.Rank
	}
	for _, s := range next {
		g.ranks[s] = rankOf[s]
	}
}

// diffSets는 old에 없고 next에 있는 종목(added)과 그 반대(removed)를 next/old의 순서대로 돌려줍니다.
func diffSets(old, next []string) (added, removed []string) {
	inOld := make(map[string]bool, len(old))
	for _, s := range old {
		inOld[s] = true
	}
	inNext := make(map[string]bool, len(next))
	for _, s := range next {
		inNext[s] = true
		if !inOld[s] {
			added = append(added, s)
		}
	}
	for _, s := range old {
		if !inNext[s] {
			removed = append(removed, s)
		}
	}
	return added, removed
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/screener; go vet ./internal/screener && go test ./internal/screener`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/screener
git commit -m "Add screener.Gate: pre-open warm-up, live refresh, fallback, lazy-expand toggle"
```

---

### Task 7: `ChaseGuard` — 이미 목표가를 크게 넘은 종목 진입 차단

장중 랭킹에 늦게 편입된 종목은 이미 급등해 있을 수 있다. 목표가 돌파는 "돌파 순간"에 사는 전략이므로, 처음 관찰한 가격이 목표가보다 `CHASE_LIMIT_PCT` 넘게 높으면 그 종목은 당일 진입 후보에서 뺀다(설계 6절). 시뮬레이터 밖에서 관찰값을 걸러내므로 청산 로직은 영향받지 않는다.

**Files:**
- Create: `internal/tradingloop/chase.go`
- Test: `internal/tradingloop/chase_test.go`

**Interfaces:**
- Consumes: `tradingloop.PriceObservation{Symbol, Price, Timestamp, Err}`, `simulator.Setup{Symbol, TargetPrice, TrendOK}`.
- Produces:
  - `func NewChaseGuard(limitPct float64) *ChaseGuard` (`0.01` = 1%; `<= 0`이면 비활성)
  - `func (c *ChaseGuard) Filter(obs []PriceObservation, setups map[string]simulator.Setup, holding bool, now time.Time, maxAge time.Duration) (kept []PriceObservation, newlyBlocked []string)`
  - `func (c *ChaseGuard) Blocked(symbol string) bool`
- 규칙:
  - `limitPct <= 0` 또는 `holding == true`면 관찰값을 그대로 돌려주고 아무것도 차단하지 않는다(보유 중에는 청산 판단이 우선).
  - 오류(`Err != nil`)이거나 `now - Timestamp > maxAge`인 관찰값은 판단 근거가 될 수 없으므로 그대로 통과시키고 차단도 하지 않는다.
  - Setup이 없거나 `TrendOK == false`인 종목은 어차피 진입하지 않으므로 그대로 통과(차단하지 않음).
  - `Price > TargetPrice*(1+limitPct)`이면 그 종목을 당일 영구 차단하고 이 관찰값을 제거한다. 이후 가격이 내려와도 차단은 유지된다(이미 놓친 종목을 뒤늦게 사지 않는다).
  - 차단된 종목의 관찰값은 이후에도 계속 제거한다.
  - `newlyBlocked`는 이번 호출에서 처음 차단된 종목만 담는다(로그 1회용).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/tradingloop/chase_test.go`:

```go
package tradingloop

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

var chaseNow = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)

func chaseObs(symbol string, price float64) PriceObservation {
	return PriceObservation{Symbol: symbol, Price: price, Timestamp: chaseNow}
}

func chaseSetups() map[string]simulator.Setup {
	return map[string]simulator.Setup{
		"A":  {Symbol: "A", TargetPrice: 100, TrendOK: true},
		"B":  {Symbol: "B", TargetPrice: 200, TrendOK: true},
		"NT": {Symbol: "NT", TargetPrice: 100, TrendOK: false},
	}
}

func symbols(obs []PriceObservation) []string {
	var out []string
	for _, o := range obs {
		out = append(out, o.Symbol)
	}
	return out
}

func TestChaseGuardBlocksSymbolFarAboveTarget(t *testing.T) {
	g := NewChaseGuard(0.01)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 102), chaseObs("B", 200)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if !reflect.DeepEqual(symbols(kept), []string{"B"}) || !reflect.DeepEqual(blocked, []string{"A"}) || !g.Blocked("A") {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardWithinLimitPasses(t *testing.T) {
	g := NewChaseGuard(0.01)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 100.9)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 1 || len(blocked) != 0 {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardBlockIsStickyAndReportedOnce(t *testing.T) {
	g := NewChaseGuard(0.01)
	g.Filter([]PriceObservation{chaseObs("A", 105)}, chaseSetups(), false, chaseNow, 30*time.Second)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 100)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 0 || len(blocked) != 0 {
		t.Fatalf("가격이 내려와도 차단은 유지되고 다시 보고되지 않아야 합니다: kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardDisabledOrHolding(t *testing.T) {
	obs := []PriceObservation{chaseObs("A", 150)}
	for name, g := range map[string]*ChaseGuard{"off": NewChaseGuard(0), "negative": NewChaseGuard(-1)} {
		if kept, blocked := g.Filter(obs, chaseSetups(), false, chaseNow, 30*time.Second); len(kept) != 1 || len(blocked) != 0 {
			t.Errorf("%s: kept=%v blocked=%v", name, symbols(kept), blocked)
		}
	}
	g := NewChaseGuard(0.01)
	if kept, blocked := g.Filter(obs, chaseSetups(), true, chaseNow, 30*time.Second); len(kept) != 1 || len(blocked) != 0 || g.Blocked("A") {
		t.Errorf("보유 중에는 걸러내면 안 됩니다: kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardIgnoresStaleErroredAndUntradable(t *testing.T) {
	g := NewChaseGuard(0.01)
	stale := PriceObservation{Symbol: "A", Price: 150, Timestamp: chaseNow.Add(-time.Minute)}
	errored := PriceObservation{Symbol: "B", Err: errors.New("503")}
	noTrend := chaseObs("NT", 150)
	unknown := chaseObs("ZZ", 150)
	kept, blocked := g.Filter([]PriceObservation{stale, errored, noTrend, unknown}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 4 || len(blocked) != 0 {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/tradingloop -run TestChaseGuard`
Expected: FAIL (`NewChaseGuard` 미정의)

- [ ] **Step 3: 구현**

`internal/tradingloop/chase.go`:

```go
package tradingloop

import (
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

// ChaseGuard는 처음 관찰한 가격이 이미 목표가를 limitPct 넘게 웃도는 종목을 당일 진입 후보에서
// 영구히 제외합니다. 시뮬레이터 앞단에서 관찰값만 거르므로 청산에는 관여하지 않습니다.
type ChaseGuard struct {
	limitPct float64
	blocked  map[string]bool
}

func NewChaseGuard(limitPct float64) *ChaseGuard {
	return &ChaseGuard{limitPct: limitPct, blocked: map[string]bool{}}
}

func (c *ChaseGuard) Blocked(symbol string) bool { return c.blocked[symbol] }

func (c *ChaseGuard) Filter(obs []PriceObservation, setups map[string]simulator.Setup, holding bool, now time.Time, maxAge time.Duration) (kept []PriceObservation, newlyBlocked []string) {
	if c.limitPct <= 0 || holding {
		return obs, nil
	}
	kept = make([]PriceObservation, 0, len(obs))
	for _, o := range obs {
		if c.blocked[o.Symbol] {
			continue
		}
		if o.Err != nil || now.Sub(o.Timestamp) > maxAge {
			kept = append(kept, o)
			continue
		}
		setup, ok := setups[o.Symbol]
		if ok && setup.TrendOK && o.Price > setup.TargetPrice*(1+c.limitPct) {
			c.blocked[o.Symbol] = true
			newlyBlocked = append(newlyBlocked, o.Symbol)
			continue
		}
		kept = append(kept, o)
	}
	return kept, newlyBlocked
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/tradingloop; go vet ./internal/tradingloop && go test ./internal/tradingloop`
Expected: PASS (기존 `ProcessTick` 테스트 포함)

- [ ] **Step 5: 커밋**

```bash
git add internal/tradingloop/chase.go internal/tradingloop/chase_test.go
git commit -m "Add ChaseGuard: skip symbols already past target by more than the chase limit"
```

---

### Task 8: 세션 저장소 — 평가 결과 영속화 (`screener_eval`)

프로세스가 장중에 재시작되어도 오늘 이미 평가(통과/탈락)한 종목을 다시 평가하지 않도록, `Entry` JSON을 (거래일, 시장, 종목) 키로 저장한다. `session` 패키지는 `screener`를 알 필요가 없도록 불투명한 `[]byte`만 다룬다.

**Files:**
- Modify: `internal/session/session.go` (`Open`의 스키마 생성 부분, 새 메서드 2개)
- Test: `internal/session/session_test.go` (테스트 추가)

**Interfaces:**
- Produces:
  - `func (s *Store) SaveEval(ctx context.Context, date, market, symbol string, data []byte) error` — 같은 키는 덮어쓴다.
  - `func (s *Store) LoadEvals(ctx context.Context, date, market string) ([][]byte, error)` — 해당 날짜·시장의 모든 항목을 종목 오름차순으로. 없으면 빈 슬라이스와 nil 오류.
- 기존 `session_state` 테이블과 메서드는 변경하지 않는다.

- [ ] **Step 1: 실패하는 테스트 작성** (`session_test.go` 끝에 추가)

```go
func TestSaveAndLoadEvals(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	got, err := store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil || len(got) != 0 {
		t.Fatalf("빈 상태: %v %v", got, err)
	}

	for _, e := range []struct{ date, market, symbol, data string }{
		{"2026-10-01", "KR", "B", `{"n":2}`},
		{"2026-10-01", "KR", "A", `{"n":1}`},
		{"2026-10-01", "US", "A", `{"n":9}`},
		{"2026-10-02", "KR", "A", `{"n":8}`},
	} {
		if err := store.SaveEval(ctx, e.date, e.market, e.symbol, []byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "A", []byte(`{"n":10}`)); err != nil { // 덮어쓰기
		t.Fatal(err)
	}

	got, err = store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{[]byte(`{"n":10}`), []byte(`{"n":2}`)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadEvals = %q, want %q", got, want)
	}
}

func TestEvalsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	ctx := context.Background()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "A", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil || len(got) != 1 || string(got[0]) != `{"n":1}` {
		t.Fatalf("재시작 후: %q %v", got, err)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/session -run 'Evals'`
Expected: FAIL (`SaveEval` 미정의)

- [ ] **Step 3: 구현**

`internal/session/session.go`의 `Open`에서 `session_state` 생성 `Exec` 다음에 추가:

```go
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS screener_eval (
		trading_date TEXT NOT NULL,
		market TEXT NOT NULL,
		symbol TEXT NOT NULL,
		eval_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (trading_date, market, symbol)
	)`); err != nil {
		db.Close()
		return nil, err
	}
```

파일 끝에 메서드 추가:

```go
// SaveEval stores one symbol's screening result for (date, market). The payload is opaque to this
// package; callers own its encoding.
func (s *Store) SaveEval(ctx context.Context, date, market, symbol string, data []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO screener_eval (trading_date, market, symbol, eval_json, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(trading_date, market, symbol) DO UPDATE SET eval_json = excluded.eval_json, updated_at = excluded.updated_at`,
		date, market, symbol, string(data))
	return err
}

// LoadEvals returns every stored screening result for (date, market), ordered by symbol.
func (s *Store) LoadEvals(ctx context.Context, date, market string) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT eval_json FROM screener_eval WHERE trading_date = ? AND market = ? ORDER BY symbol ASC`, date, market)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, []byte(data))
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/session; go vet ./internal/session && go test ./internal/session`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/session
git commit -m "Persist screener evaluations per (date, market, symbol) for restart recovery"
```

---

### Task 9: 대시보드 스냅샷에 스크리너 상태 표시

**Files:**
- Modify: `internal/snapshot/snapshot.go` (`Snapshot` 필드 1개, 타입 3개, 템플릿 섹션 1개)
- Test: `internal/snapshot/snapshot_test.go` (테스트 추가)

**Interfaces:**
- Produces:
  - `type ScreenerActive struct { Symbol string; Rank int; Target float64; Origin string }` (JSON: `symbol`, `rank`, `target`, `origin`)
  - `type ScreenerRejection struct { Symbol string; Reason string }` (JSON: `symbol`, `reason`)
  - `type ScreenerStatus struct { Active []ScreenerActive; Rejections []ScreenerRejection }` (JSON: `active`, `rejections`)
  - `Snapshot.Screener *ScreenerStatus` (JSON: `screener,omitempty`)
- `Screener == nil`이면 JSON에서 필드가 빠지고 HTML에서 섹션이 나오지 않는다(미국 시장 등 스크리너가 없는 경로, 그리고 기존 테스트가 그대로 통과해야 함). 문자열은 모두 `html/template`이 이스케이프한다.

- [ ] **Step 1: 실패하는 테스트 작성** (`snapshot_test.go` 끝에 추가)

```go
func TestScreenerStatusJSONAndHTML(t *testing.T) {
	attack := "<script>alert(1)</script>"
	s := Snapshot{
		UpdatedAt: time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC),
		Screener: &ScreenerStatus{
			Active:     []ScreenerActive{{Symbol: attack, Rank: 3, Target: 51200, Origin: attack}},
			Rejections: []ScreenerRejection{{Symbol: "LEV1", Reason: attack}},
		},
	}
	data, err := s.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Screener, s.Screener) {
		t.Errorf("JSON 왕복 결과: %+v, 기대: %+v", got.Screener, s.Screener)
	}

	html, err := s.RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>") {
		t.Fatal("실행 가능한 스크립트가 포함되었습니다")
	}
	if got := strings.Count(string(html), "&lt;script&gt;alert(1)&lt;/script&gt;"); got != 3 {
		t.Errorf("이스케이프된 필드 수: %d, 기대: 3", got)
	}
	for _, want := range []string{"활성 종목", "51200", "LEV1", "탈락 종목"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("필수 표시 내용 누락: %s", want)
		}
	}
}

func TestScreenerAbsentWhenNil(t *testing.T) {
	data, err := (Snapshot{}).RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "screener") {
		t.Errorf("nil일 때 JSON에 screener가 없어야 합니다: %s", data)
	}
	html, err := (Snapshot{}).RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "활성 종목") {
		t.Error("nil일 때 스크리너 섹션이 없어야 합니다")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/snapshot -run Screener`
Expected: FAIL (`ScreenerStatus` 미정의)

- [ ] **Step 3: 구현**

`snapshot.go`의 `Snapshot` 정의 앞에 타입 추가:

```go
// ScreenerActive는 오늘 감시 중인 활성 종목입니다.
type ScreenerActive struct {
	Symbol string  `json:"symbol"`
	Rank   int     `json:"rank"`
	Target float64 `json:"target"`
	Origin string  `json:"origin"`
}

// ScreenerRejection은 오늘 평가에서 탈락한 종목과 사유입니다.
type ScreenerRejection struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}

// ScreenerStatus는 일일 스크리너의 현재 상태입니다. nil이면 표시하지 않습니다.
type ScreenerStatus struct {
	Active     []ScreenerActive    `json:"active"`
	Rejections []ScreenerRejection `json:"rejections"`
}
```

`Snapshot` 구조체의 `UpdatedAt` 다음 줄에 추가:

```go
	Screener          *ScreenerStatus  `json:"screener,omitempty"`
```

템플릿에서 `</tbody></table>` 뒤(최근 주문 표 다음)이자 `</body>` 앞에 추가:

```
{{with .Screener}}
<h2>활성 종목</h2>
<table><thead><tr><th>순위</th><th>종목</th><th>목표가</th><th>편입 경로</th></tr></thead><tbody>
{{range .Active}}<tr><td>{{.Rank}}</td><td>{{.Symbol}}</td><td>{{.Target}}</td><td>{{.Origin}}</td></tr>
{{else}}<tr><td colspan="4">활성 종목 없음</td></tr>{{end}}
</tbody></table>
<h2>탈락 종목</h2>
<table><thead><tr><th>종목</th><th>사유</th></tr></thead><tbody>
{{range .Rejections}}<tr><td>{{.Symbol}}</td><td>{{.Reason}}</td></tr>
{{else}}<tr><td colspan="2">탈락 종목 없음</td></tr>{{end}}
</tbody></table>
{{end}}
```

(`RenderHTML`의 익명 구조체는 `Snapshot`을 임베드하므로 `.Screener`가 그대로 보인다. `{{with}}` 안에서는 `.`이 `*ScreenerStatus`로 바뀌므로 `.Active`/`.Rejections`로 접근한다.)

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l internal/snapshot; go vet ./internal/snapshot && go test ./internal/snapshot`
Expected: PASS (기존 `TestRenderHTML`의 이스케이프 개수 5 유지)

- [ ] **Step 5: 커밋**

```bash
git add internal/snapshot
git commit -m "Show screener active set and rejections on the dashboard snapshot"
```

---

### Task 10: `cmd/trader/screener.go` — 스크리너를 거래 루프에 붙이는 헬퍼

`main.go`는 다음 Task에서 고친다. 이 Task는 `main.go`가 부를 작은 함수들을 테스트와 함께 먼저 만든다(이 시점에는 어디서도 호출하지 않아도 컴파일된다).

**Files:**
- Create: `cmd/trader/screener.go`
- Test: `cmd/trader/screener_test.go`

**Interfaces:**
- Consumes: `screener.Config/DefaultConfig/Entry/Evaluator/Update`, `session.Store.SaveEval/LoadEvals`, `snapshot.ScreenerStatus`, `tradingloop.PriceObservation`, `simulator.Setup`, `tossapi.Price`.
- Produces:
  - `func screenerConfig(market string, commissionRate float64, getenv func(string) string) screener.Config` — 환경변수 덮어쓰기. 잘못된 값(숫자 아님, 음수, `RankDepth`/`ActiveCount` 0 이하, `NOISE_MIN >= NOISE_MAX`)은 기본값을 쓴다.
    - `RANK_DEPTH`, `ACTIVE_COUNT`, `EVAL_PER_MIN`(0=무제한), `ACTIVE_KEEP_RANK`(0=끔), `NOISE_MIN`/`NOISE_MAX`(퍼센트, `2.5` → 0.025), `RANK_START_DELAY_MINUTES`, `RANK_REFRESH_SECONDS`, `LAZY_EXPAND`(`false`/`0`이면 끔)
  - `func chaseLimitFromEnv(getenv func(string) string) float64` — `CHASE_LIMIT_PCT`(퍼센트, 기본 1 → 0.01, 0이면 비활성)
  - `func pollSymbols(active []string, held string) []string` — 보유 종목이 활성 집합에 없으면 맨 앞에 붙인다(중복 없음).
  - `func setupsFor(setupOf func(string) (simulator.Setup, bool), symbols []string, held string) map[string]simulator.Setup` — 보유 종목은 Setup이 없어도 `simulator.Setup{Symbol: held}`(진입 불가, 손절·마감 청산용)를 항상 넣는다.
  - `type priceSource interface{ Price(ctx context.Context, symbol string) (*tossapi.Price, error) }` 와 `func pollPrices(ctx context.Context, src priceSource, symbols []string) []tradingloop.PriceObservation` — 기존 `pollWatchlist` 로직을 종목 목록 인자로 일반화(주어진 순서 유지).
  - `func restoreEvals(ctx context.Context, store *session.Store, ev *screener.Evaluator, date, market string) (restored, skipped int, err error)` — 저장된 평가를 `ev.Seed`로 복구. 깨진 JSON은 건너뛰고 `skipped`로 센다.
  - `func evalSaver(ctx context.Context, store *session.Store, date, market string) func(screener.Entry)` — `Evaluator.OnEvaluated`에 꽂는 저장 콜백(실패는 로그만).
  - `func buildScreenerStatus(active []string, rank func(string) int, entries []screener.Entry, maxRejections int) *snapshot.ScreenerStatus`
  - `func describeUpdate(up screener.Update, rank func(string) int) []string` — 로그 줄.

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/trader/screener_test.go`:

```go
package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestScreenerConfigDefaultsAndOverrides(t *testing.T) {
	def := screener.DefaultConfig("KR")
	def.CommissionRate = 0.001
	if got := screenerConfig("KR", 0.001, envOf(nil)); !reflect.DeepEqual(got, def) {
		t.Fatalf("기본값: %+v, 기대: %+v", got, def)
	}
	got := screenerConfig("KR", 0.001, envOf(map[string]string{
		"RANK_DEPTH": "40", "ACTIVE_COUNT": "8", "EVAL_PER_MIN": "0", "ACTIVE_KEEP_RANK": "20",
		"NOISE_MIN": "3", "NOISE_MAX": "5.5", "RANK_START_DELAY_MINUTES": "10", "RANK_REFRESH_SECONDS": "30",
		"LAZY_EXPAND": "false",
	}))
	if got.RankDepth != 40 || got.ActiveCount != 8 || got.EvalPerMin != 0 || got.KeepRank != 20 ||
		math.Abs(got.NoiseMin-0.03) > 1e-12 || math.Abs(got.NoiseMax-0.055) > 1e-12 ||
		got.StartDelay != 10*time.Minute || got.RefreshEvery != 30*time.Second || got.LazyExpand {
		t.Errorf("덮어쓰기 결과: %+v", got)
	}
}

func TestScreenerConfigRejectsBadValues(t *testing.T) {
	def := screenerConfig("KR", 0, envOf(nil))
	for name, env := range map[string]map[string]string{
		"not a number":   {"RANK_DEPTH": "abc", "ACTIVE_COUNT": "x"},
		"zero active":    {"ACTIVE_COUNT": "0"},
		"negative depth": {"RANK_DEPTH": "-5"},
		"inverted noise": {"NOISE_MIN": "6", "NOISE_MAX": "2"},
		"negative delay": {"RANK_START_DELAY_MINUTES": "-1"},
		"zero refresh":   {"RANK_REFRESH_SECONDS": "0"},
	} {
		if got := screenerConfig("KR", 0, envOf(env)); !reflect.DeepEqual(got, def) {
			t.Errorf("%s: 잘못된 값이 기본값을 대체했습니다: %+v", name, got)
		}
	}
}

func TestChaseLimitFromEnv(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want float64
	}{
		{nil, 0.01},
		{map[string]string{"CHASE_LIMIT_PCT": "2.5"}, 0.025},
		{map[string]string{"CHASE_LIMIT_PCT": "0"}, 0},
		{map[string]string{"CHASE_LIMIT_PCT": "-1"}, 0.01},
		{map[string]string{"CHASE_LIMIT_PCT": "abc"}, 0.01},
	} {
		if got := chaseLimitFromEnv(envOf(tc.env)); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("%v: %v, 기대: %v", tc.env, got, tc.want)
		}
	}
}

func TestPollSymbolsAlwaysIncludesHeld(t *testing.T) {
	cases := []struct {
		active []string
		held   string
		want   []string
	}{
		{[]string{"A", "B"}, "", []string{"A", "B"}},
		{[]string{"A", "B"}, "B", []string{"A", "B"}},
		{[]string{"A", "B"}, "Z", []string{"Z", "A", "B"}},
		{nil, "Z", []string{"Z"}},
		{nil, "", nil},
	}
	for _, tc := range cases {
		if got := pollSymbols(tc.active, tc.held); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pollSymbols(%v, %q) = %v, 기대: %v", tc.active, tc.held, got, tc.want)
		}
	}
}

func TestSetupsForGivesHeldSymbolAFallback(t *testing.T) {
	known := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 100, TrendOK: true}}
	setupOf := func(s string) (simulator.Setup, bool) { v, ok := known[s]; return v, ok }

	got := setupsFor(setupOf, []string{"A", "B"}, "")
	if len(got) != 1 || got["A"].TargetPrice != 100 {
		t.Errorf("Setup이 없는 종목은 빠져야 합니다: %+v", got)
	}
	got = setupsFor(setupOf, []string{"Z", "A"}, "Z")
	if z, ok := got["Z"]; !ok || z.TrendOK || z.Symbol != "Z" {
		t.Errorf("보유 종목 대체 Setup: %+v %v", z, ok)
	}
}

type fakePrices map[string]*tossapi.Price

func (f fakePrices) Price(_ context.Context, symbol string) (*tossapi.Price, error) {
	p, ok := f[symbol]
	if !ok {
		return nil, errors.New("503")
	}
	return p, nil
}

func TestPollPricesKeepsOrderAndReportsErrors(t *testing.T) {
	src := fakePrices{
		"A": {LastPrice: "100", Timestamp: "2026-10-01T01:00:00Z"},
		"C": {LastPrice: "abc", Timestamp: "2026-10-01T01:00:00Z"},
		"D": {LastPrice: "100", Timestamp: "not-a-time"},
	}
	obs := pollPrices(context.Background(), src, []string{"A", "B", "C", "D"})
	if len(obs) != 4 {
		t.Fatalf("관찰값 %d개", len(obs))
	}
	if obs[0].Symbol != "A" || obs[0].Err != nil || obs[0].Price != 100 {
		t.Errorf("A: %+v", obs[0])
	}
	for i, sym := range []string{"B", "C", "D"} {
		if obs[i+1].Symbol != sym || obs[i+1].Err == nil {
			t.Errorf("%s는 오류로 보고되어야 합니다: %+v", sym, obs[i+1])
		}
	}
}

func TestRestoreAndSaveEvals(t *testing.T) {
	ctx := context.Background()
	store, err := session.Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	save := evalSaver(ctx, store, "2026-10-01", "KR")
	save(screener.Entry{Symbol: "A", Passed: true, HasOpen: true, Open: 110, Target: 112, TrendOK: true})
	save(screener.Entry{Symbol: "B", Passed: false, Reason: "레버리지/인버스"})
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "BAD", []byte("not json")); err != nil {
		t.Fatal(err)
	}

	ev := screener.NewEvaluator(nil, screener.DefaultConfig("KR"), time.Now())
	restored, skipped, err := restoreEvals(ctx, store, ev, "2026-10-01", "KR")
	if err != nil || restored != 2 || skipped != 1 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	if a, ok := ev.Cached("A"); !ok || a.Target != 112 || !a.HasOpen {
		t.Errorf("A 복구: %+v %v", a, ok)
	}
	if b, ok := ev.Cached("B"); !ok || b.Passed || b.Reason == "" {
		t.Errorf("탈락 항목도 복구되어야 합니다: %+v %v", b, ok)
	}
}

func TestBuildScreenerStatus(t *testing.T) {
	entries := []screener.Entry{
		{Symbol: "A", Passed: true, Origin: "prewarm", Target: 112},
		{Symbol: "B", Passed: true, Origin: "intraday", Target: 55},
		{Symbol: "X", Passed: false, Reason: "노이즈 1.2% (2.5~6%)"},
		{Symbol: "Y", Passed: false, Reason: "레버리지/인버스"},
		{Symbol: "Z", Passed: false, Reason: "유의사항 1건"},
	}
	rank := func(s string) int { return map[string]int{"A": 2, "B": 5}[s] }
	st := buildScreenerStatus([]string{"A", "B"}, rank, entries, 2)
	if len(st.Active) != 2 || st.Active[0].Symbol != "A" || st.Active[0].Rank != 2 || st.Active[0].Target != 112 || st.Active[1].Origin != "intraday" {
		t.Errorf("Active = %+v", st.Active)
	}
	if len(st.Rejections) != 2 || st.Rejections[0].Symbol != "X" || st.Rejections[1].Symbol != "Y" {
		t.Errorf("Rejections는 종목순으로 상한까지만: %+v", st.Rejections)
	}
	if empty := buildScreenerStatus(nil, rank, nil, 5); empty == nil || len(empty.Active) != 0 {
		t.Errorf("빈 상태에서도 nil이 아니어야 합니다: %+v", empty)
	}
}

func TestDescribeUpdate(t *testing.T) {
	rank := func(string) int { return 3 }
	if got := describeUpdate(screener.Update{}, rank); len(got) != 0 {
		t.Errorf("Ran=false는 로그가 없어야 합니다: %v", got)
	}
	lines := describeUpdate(screener.Update{Ran: true, Added: []string{"A"}, Removed: []string{"B"}, Skipped: 2,
		UsedFallback: true, Errs: []error{errors.New("랭킹 조회: 503")}}, rank)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"편입", "A", "제외", "B", "예산", "2", "사전 평가", "503"} {
		if !strings.Contains(joined, want) {
			t.Errorf("로그에 %q가 없습니다:\n%s", want, joined)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./cmd/trader -run 'Screener|ChaseLimit|PollSymbols|SetupsFor|PollPrices|Evals|DescribeUpdate'`
Expected: FAIL (정의되지 않은 함수)

- [ ] **Step 3: 구현**

`cmd/trader/screener.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

func envFloat(getenv func(string) string, key string) (float64, bool) {
	v := getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 || n != n {
		return 0, false
	}
	return n, true
}

func envInt(getenv func(string) string, key string, min int) (int, bool) {
	v := getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, false
	}
	return n, true
}

func screenerConfig(market string, commissionRate float64, getenv func(string) string) screener.Config {
	c := screener.DefaultConfig(market)
	c.CommissionRate = commissionRate
	if n, ok := envInt(getenv, "RANK_DEPTH", 1); ok {
		c.RankDepth = n
	}
	if n, ok := envInt(getenv, "ACTIVE_COUNT", 1); ok {
		c.ActiveCount = n
	}
	if n, ok := envInt(getenv, "EVAL_PER_MIN", 0); ok {
		c.EvalPerMin = n
	}
	if n, ok := envInt(getenv, "ACTIVE_KEEP_RANK", 0); ok {
		c.KeepRank = n
	}
	if n, ok := envInt(getenv, "RANK_START_DELAY_MINUTES", 0); ok {
		c.StartDelay = time.Duration(n) * time.Minute
	}
	if n, ok := envInt(getenv, "RANK_REFRESH_SECONDS", 1); ok {
		c.RefreshEvery = time.Duration(n) * time.Second
	}
	lo, hi := c.NoiseMin, c.NoiseMax
	if v, ok := envFloat(getenv, "NOISE_MIN"); ok {
		lo = v / 100
	}
	if v, ok := envFloat(getenv, "NOISE_MAX"); ok {
		hi = v / 100
	}
	if lo < hi {
		c.NoiseMin, c.NoiseMax = lo, hi
	}
	if v := getenv("LAZY_EXPAND"); v == "false" || v == "0" {
		c.LazyExpand = false
	}
	return c
}

func chaseLimitFromEnv(getenv func(string) string) float64 {
	if v, ok := envFloat(getenv, "CHASE_LIMIT_PCT"); ok {
		return v / 100
	}
	return 0.01
}

func pollSymbols(active []string, held string) []string {
	if held == "" {
		return active
	}
	for _, s := range active {
		if s == held {
			return active
		}
	}
	return append([]string{held}, active...)
}

func setupsFor(setupOf func(string) (simulator.Setup, bool), symbols []string, held string) map[string]simulator.Setup {
	setups := make(map[string]simulator.Setup, len(symbols))
	for _, s := range symbols {
		if setup, ok := setupOf(s); ok {
			setups[s] = setup
		}
	}
	if _, ok := setups[held]; held != "" && !ok {
		setups[held] = simulator.Setup{Symbol: held}
	}
	return setups
}

type priceSource interface {
	Price(ctx context.Context, symbol string) (*tossapi.Price, error)
}

func pollPrices(ctx context.Context, src priceSource, symbols []string) []tradingloop.PriceObservation {
	observations := make([]tradingloop.PriceObservation, 0, len(symbols))
	for _, symbol := range symbols {
		price, err := src.Price(ctx, symbol)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: symbol, Err: err})
			continue
		}
		last, err := strconv.ParseFloat(price.LastPrice, 64)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: symbol, Err: err})
			continue
		}
		ts, err := time.Parse(time.RFC3339, price.Timestamp)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: symbol, Err: err})
			continue
		}
		observations = append(observations, tradingloop.PriceObservation{Symbol: symbol, Price: last, Timestamp: ts})
	}
	return observations
}

func restoreEvals(ctx context.Context, store *session.Store, ev *screener.Evaluator, date, market string) (restored, skipped int, err error) {
	blobs, err := store.LoadEvals(ctx, date, market)
	if err != nil {
		return 0, 0, err
	}
	entries := make([]screener.Entry, 0, len(blobs))
	for _, blob := range blobs {
		var e screener.Entry
		if err := json.Unmarshal(blob, &e); err != nil || e.Symbol == "" {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	ev.Seed(entries)
	return len(entries), skipped, nil
}

func evalSaver(ctx context.Context, store *session.Store, date, market string) func(screener.Entry) {
	return func(e screener.Entry) {
		data, err := json.Marshal(e)
		if err == nil {
			err = store.SaveEval(ctx, date, market, e.Symbol, data)
		}
		if err != nil {
			log.Printf("%s 평가 결과 저장 실패(재시작 시 다시 평가합니다): %v", e.Symbol, err)
		}
	}
}

func buildScreenerStatus(active []string, rank func(string) int, entries []screener.Entry, maxRejections int) *snapshot.ScreenerStatus {
	bysymbol := make(map[string]screener.Entry, len(entries))
	for _, e := range entries {
		bysymbol[e.Symbol] = e
	}
	st := &snapshot.ScreenerStatus{}
	for _, s := range active {
		e := bysymbol[s]
		st.Active = append(st.Active, snapshot.ScreenerActive{Symbol: s, Rank: rank(s), Target: e.Target, Origin: e.Origin})
	}
	for _, e := range entries {
		if e.Passed || len(st.Rejections) >= maxRejections {
			continue
		}
		st.Rejections = append(st.Rejections, snapshot.ScreenerRejection{Symbol: e.Symbol, Reason: e.Reason})
	}
	return st
}

func describeUpdate(up screener.Update, rank func(string) int) []string {
	if !up.Ran {
		return nil
	}
	var lines []string
	if len(up.Added) > 0 {
		parts := make([]string, len(up.Added))
		for i, s := range up.Added {
			parts[i] = fmt.Sprintf("%s(순위 %d)", s, rank(s))
		}
		lines = append(lines, "활성 종목 편입: "+strings.Join(parts, ", "))
	}
	if len(up.Removed) > 0 {
		lines = append(lines, "활성 종목 제외: "+strings.Join(up.Removed, ", "))
	}
	if up.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("분당 평가 예산 때문에 %d개 종목 평가를 다음 갱신으로 미뤘습니다", up.Skipped))
	}
	if up.UsedFallback {
		lines = append(lines, "당일 랭킹을 한 번도 받지 못해 사전 평가 통과 종목을 활성 종목으로 대신 사용합니다")
	}
	for _, err := range up.Errs {
		lines = append(lines, fmt.Sprintf("스크리너 갱신 중 오류(직전 활성 집합 유지): %v", err))
	}
	return lines
}
```

- [ ] **Step 4: 통과 확인**

Run: `gofmt -l cmd/trader; go vet ./cmd/trader && go test ./cmd/trader`
Expected: PASS (기존 `main_test.go` 포함). `pollWatchlist`가 아직 남아 있어도 컴파일된다.

- [ ] **Step 5: 커밋**

```bash
git add cmd/trader/screener.go cmd/trader/screener_test.go
git commit -m "Add trader-side screener helpers: env config, held-symbol polling, eval persistence"
```

---

### Task 11: `cmd/trader/main.go` 통합, 옛 워치리스트 이동

**Files:**
- Modify: `cmd/trader/screener.go` (`describeEntry` 추가), `cmd/trader/screener_test.go`
- Modify: `cmd/trader/main.go`
- Move: `internal/strategy/watchlist.go` → `cmd/backtest/watchlist.go`
- Delete: `internal/strategy/watchlist_test.go`
- Modify: `cmd/backtest/main.go` (참조 2곳, 주석 1곳)

**Interfaces:**
- Consumes: Task 3~10의 모든 공개 API.
- Produces: 동작 변화만 있고 새 공개 API는 없다. `buildSnapshot`/`publishSnapshot`에 마지막 인자 `status *snapshot.ScreenerStatus`가 추가된다.

- [ ] **Step 1: `describeEntry` 테스트 작성** (`cmd/trader/screener_test.go` 끝에 추가)

이 로그 줄은 내일(10/1) 부팅 로그 검증에 쓰이므로 기존 `셋업: 시가=… 목표가=…` 형태를 유지한다.

```go
func TestDescribeEntry(t *testing.T) {
	ready := describeEntry(screener.Entry{Symbol: "005930", Origin: "prewarm", Passed: true, HasOpen: true,
		Open: 110, Target: 112, TrendOK: true, NoiseRatio: 0.035, Prev: strategy.DailyBar{Date: "2026-09-30", High: 114, Low: 108, Close: 111}})
	for _, want := range []string{"005930", "셋업: 시가=110.00", "목표가=112.00", "prewarm", "추세=true", "3.50%"} {
		if !strings.Contains(ready, want) {
			t.Errorf("%q가 없습니다: %s", want, ready)
		}
	}
	waiting := describeEntry(screener.Entry{Symbol: "A", Origin: "intraday", Passed: true, NoiseRatio: 0.04})
	if !strings.Contains(waiting, "시가 대기") || strings.Contains(waiting, "셋업:") {
		t.Errorf("시가 대기 줄: %s", waiting)
	}
	rejected := describeEntry(screener.Entry{Symbol: "B", Origin: "intraday", Reason: "레버리지/인버스"})
	if !strings.Contains(rejected, "탈락") || !strings.Contains(rejected, "레버리지/인버스") {
		t.Errorf("탈락 줄: %s", rejected)
	}
}
```

`screener_test.go`의 import에 `"github.com/tibetkowon/toss-trader/internal/strategy"`를 추가한다.

- [ ] **Step 2: 실패 확인 후 `describeEntry` 구현**

Run: `go test ./cmd/trader -run TestDescribeEntry` → FAIL(미정의).

`cmd/trader/screener.go`에 추가:

```go
func describeEntry(e screener.Entry) string {
	switch {
	case !e.Passed:
		return fmt.Sprintf("%s 평가 탈락(%s): %s", e.Symbol, e.Origin, e.Reason)
	case e.HasOpen:
		return fmt.Sprintf("%s 셋업: 시가=%.2f 전일(%s) 고저=%.2f/%.2f 종가=%.2f 노이즈=%.2f%% 추세=%v 목표가=%.2f (%s)",
			e.Symbol, e.Open, e.Prev.Date, e.Prev.High, e.Prev.Low, e.Prev.Close, e.NoiseRatio*100, e.TrendOK, e.Target, e.Origin)
	default:
		return fmt.Sprintf("%s 평가 통과, 정규장 시가 대기(%s): 노이즈=%.2f%%", e.Symbol, e.Origin, e.NoiseRatio*100)
	}
}
```

Run again → PASS.

- [ ] **Step 3: 옛 워치리스트를 백테스트로 이동**

```bash
git mv internal/strategy/watchlist.go cmd/backtest/watchlist.go
git rm internal/strategy/watchlist_test.go
```

`cmd/backtest/watchlist.go` 첫 줄 패키지 선언과 패키지 주석을 다음으로 바꾼다(나머지 `WatchlistEntry`/`Watchlist` 정의는 그대로):

```go
// 옛 고정 워치리스트(2026-09-27 선정, SPEC.md 3.3 v15에서 폐기). 6.2 백테스트 결과의 근거이자
// cmd/backtest의 기본 종목 목록으로만 보존합니다. 라이브 거래는 internal/screener를 씁니다.
package main
```

`cmd/backtest/main.go`: 50~51행의 `strategy.Watchlist` 두 곳을 `Watchlist`로 바꾸고, 파일 맨 위 주석의 `(internal/strategy.Watchlist)`를 `(cmd/backtest/watchlist.go의 Watchlist)`로 바꾼다. `strategy` import는 `MovingAverage` 등에 계속 쓰이므로 남긴다.

Run: `go build ./... && go test ./cmd/backtest ./internal/strategy`
Expected: PASS

- [ ] **Step 4: `main.go` 통합**

(a) import: `"github.com/tibetkowon/toss-trader/internal/screener"` 추가, `".../internal/strategy"` 제거(다른 곳에서 안 쓰게 된다).

(b) `runTradingSession`에서 `eodCutoff := ...` 바로 아래부터 `sim, err := loadOrStartSimulator(...)`까지를 다음으로 교체한다. 핵심 순서: 평가 캐시 복구 → 사전 평가(장 시작 전 대기 시간 활용) → 세션 시작 대기 → 시뮬레이터 생성 → 게이트에 시드 전달.

```go
	sessionDate := sessionStart.Format(dateLayout)
	scfg := screenerConfig(market, commissionRate, os.Getenv)
	ev := screener.NewEvaluator(client, scfg, sessionStart)
	if restored, skipped, err := restoreEvals(ctx, store, ev, sessionDate, market); err != nil {
		log.Printf("저장된 평가 복구 실패(처음부터 평가합니다): %v", err)
	} else if restored+skipped > 0 {
		log.Printf("저장된 평가 %d개를 복구했습니다(깨진 항목 %d개 건너뜀)", restored, skipped)
	}
	saveEval := evalSaver(ctx, store, sessionDate, market)
	ev.OnEvaluated = func(e screener.Entry) {
		log.Println(describeEntry(e))
		saveEval(e)
	}
	gate := screener.NewGate(scfg, client, ev, sessionStart)
	chase := tradingloop.NewChaseGuard(chaseLimitFromEnv(os.Getenv))
	log.Printf("스크리너 설정: %+v, 추격 상한 %.2f%%", scfg, chaseLimitFromEnv(os.Getenv)*100)

	// 장 시작 전 대기 시간에 어제 랭킹 상위 종목을 미리 평가합니다(정규장 시작 30초 전까지).
	res := gate.PreWarm(ctx, sessionStart.Add(-30*time.Second))
	log.Printf("사전 평가: 랭킹 %d개(상위 %v) 중 %d개 평가, %d개 통과, 시간초과=%v, 랭킹 오류=%v, 종목 오류 %d건",
		res.Ranked, res.Top, res.Evaluated, res.Passed, res.TimedOut, res.Err, len(res.SymbolErrs))

	// 오늘 시가는 정규장이 실제로 시작된 뒤에만 의미가 있습니다(장 시작 전 Price()는
	// 전일 마지막 체결가를 반환) — Cloud Scheduler가 버퍼를 두고 미리 기동하므로
	// (SPEC.md 2.1) 여기서 세션 시작까지 대기합니다.
	if wait := time.Until(sessionStart) + 5*time.Second; wait > 0 {
		log.Printf("정규장 시작까지 %s 대기합니다", wait.Round(time.Second))
		time.Sleep(wait)
	}

	today := time.Now().Format(dateLayout)
	cfg := simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: commissionRate}

	sim, err := loadOrStartSimulator(ctx, store, cfg, today, market)
	if err != nil {
		return err
	}
	gate.SetSeed(sim.State().Seed)
```

(c) `setups := todaySetups(ctx, client, sessionStart)` 줄을 삭제하고, `publish` 클로저를 상태 인자를 넘기도록 바꾼다.

```go
	screenerStatus := func() *snapshot.ScreenerStatus {
		return buildScreenerStatus(gate.Active(), gate.Rank, ev.Entries(), 30)
	}
	publish := func(halted bool, reason string) {
		publishSnapshot(ctx, uploader, sim, client, halted, reason, recentOrders, screenerStatus())
	}
```

(d) 폴링 루프 앞부분(`now := time.Now()`부터 `actions := tradingloop.ProcessTick(...)`까지)을 교체한다.

```go
		now := time.Now()
		held := ""
		if pos, ok := sim.Position(); ok {
			held = pos.Symbol
		}
		for _, line := range describeUpdate(gate.Refresh(ctx, now), gate.Rank) {
			log.Println(line)
		}
		symbols := pollSymbols(gate.Active(), held)
		setups := setupsFor(gate.Setup, symbols, held)
		observations := pollPrices(ctx, client, symbols)
		for _, line := range describeSkippedObservations(observations, now, staleness) {
			log.Println(line)
		}
		observations, blocked := chase.Filter(observations, setups, held != "", now, staleness)
		for _, symbol := range blocked {
			log.Printf("추격 상한 초과로 오늘 진입에서 제외: %s (목표가 %.2f)", symbol, setups[symbol].TargetPrice)
		}
		actions := tradingloop.ProcessTick(sim, setups, observations, now, staleness)
```

(e) 마감 강제청산: `eodAction := sim.OnTick(setups[pos.Symbol], last, true)`를 다음으로 교체한다.

```go
		eodSetup := setupsFor(gate.Setup, nil, pos.Symbol)[pos.Symbol]
		eodAction := sim.OnTick(eodSetup, last, true)
```

(f) 마감 히스토리 스냅샷: `buildSnapshot(ctx, sim, client, false, "", recentOrders)` 호출에 `screenerStatus()` 인자를 추가한다.

(g) 함수 삭제: `todaySetups`, `regularSessionOpen`, `mean`, `pollWatchlist`.

(h) `buildSnapshot`/`publishSnapshot` 시그니처 끝에 `status *snapshot.ScreenerStatus`를 추가하고, `buildSnapshot`의 `s := snapshot.Snapshot{...}` 리터럴에 `Screener: status,`를 넣는다. `publishSnapshot`은 `buildSnapshot(..., recentOrders, status)`로 넘긴다.

- [ ] **Step 5: 빌드·전체 테스트**

Run: `gofmt -l . ; go vet ./... && go test ./...`
Expected: 출력 없음(gofmt), 전 패키지 PASS. 컴파일 오류가 나면 미사용 import(`strategy`, 필요 시 `strconv`)를 정리한다.

- [ ] **Step 6: 로컬 스모크 확인 (API 호출 없이 가능한 범위)**

Run: `go build -o /tmp/trader ./cmd/trader && rm /tmp/trader`
Expected: 빌드 성공. 실제 API는 IP 허용목록 때문에 VM에서만 동작하므로 라이브 검증은 Task 12의 배포 절차로 한다.

- [ ] **Step 7: 커밋**

```bash
git add -A cmd internal
git status --short   # 의도한 파일만 있는지 확인 (환경 파일·버킷 이름 등 비밀 없음)
git commit -m "Wire the daily screener into the trader loop; move the old watchlist to cmd/backtest"
```

---

### Task 12: SPEC.md·메모리 반영, 배포·검증 절차

코드 Task가 모두 끝난 뒤 문서를 실제 구현에 맞추고, VM에 배포해 첫 부팅 로그로 검증한다.

**Files:**
- Modify: `SPEC.md`
- Modify: `/Users/kowon/.claude/projects/-Volumes-MAC-Project-toss-trader/memory/project-status.md` (저장소 밖 — 커밋 대상 아님)

- [ ] **Step 1: SPEC.md 수정**

1. 3.3절 "이전 고정 워치리스트" 문단의 `코드는 internal/strategy/watchlist.go(Watchlist 변수 …)`를 `코드는 cmd/backtest/watchlist.go(Watchlist 변수 …, 백테스트 전용으로 이동)`로 바꾼다.
2. 3.3절 v15 방침 목록 끝(`전략의 진입 규칙(3.1)…` 줄 바로 앞)에 다음 표를 추가한다. 값은 모두 환경변수로 덮어쓸 수 있고, 잘못된 값은 기본값으로 대체된다.

```markdown
| 환경변수 | 기본값 | 의미 |
|---|---|---|
| `RANK_DEPTH` | 30 | 장중 랭킹 조회 깊이 |
| `ACTIVE_COUNT` | 10 | 활성 종목 수 |
| `NOISE_MIN` / `NOISE_MAX` | 2.5 / 6 (%) | 노이즈 구간 |
| `RANK_START_DELAY_MINUTES` | 5 | 개장 후 첫 갱신까지 |
| `RANK_REFRESH_SECONDS` | 60 | 활성 집합 갱신 간격 |
| `ACTIVE_KEEP_RANK` | 0 (끔) | 히스테리시스: 이 순위 밖으로 밀릴 때까지 활성 유지 |
| `CHASE_LIMIT_PCT` | 1 (%) | 추격 상한, 0이면 끔 |
| `EVAL_PER_MIN` | 3 | 분당 신규 종목 평가 상한, 0이면 무제한 |
| `LAZY_EXPAND` | 켬 | `false`/`0`이면 첫 갱신의 상위 종목으로 후보 고정 |
```

3. 6.2 또는 11절 검증 상태에 "스크리너 라이브 구현 완료, 백테스트로 설정 확정 전(2026-09-30 기준 기본값으로 라이브 가동)"을 한 줄 추가한다.

- [ ] **Step 2: 커밋**

```bash
git add SPEC.md
git commit -m "SPEC: document screener env vars and the moved legacy watchlist"
```

- [ ] **Step 3: 배포 (장 마감 후, 세션 진행 중에는 금지)**

기존 배포 방식(2026-09-30 수정본과 동일)을 그대로 따른다: 새 바이너리를 임시 경로로 올린 뒤 원자적으로 교체하고 이전 바이너리는 `trader.prev`로 남긴다. 실행 중인 프로세스는 재시작하지 않으며 다음 부팅부터 적용된다. `/etc/toss-trader/env`는 읽거나 출력하지 않는다.

```bash
gcloud compute ssh micro-trading-vm --project micro-trading-495614 --zone asia-northeast3-a --command 'uname -m'
GOOS=linux GOARCH=<위 결과에 맞춤: x86_64→amd64, aarch64→arm64> go build -o /tmp/trader-linux ./cmd/trader
gcloud compute scp /tmp/trader-linux micro-trading-vm:/tmp/trader.new --project micro-trading-495614 --zone asia-northeast3-a
gcloud compute ssh micro-trading-vm --project micro-trading-495614 --zone asia-northeast3-a --command \
  'sudo cp /opt/toss-trader/trader /opt/toss-trader/trader.prev && sudo install -m 0755 /tmp/trader.new /opt/toss-trader/trader.tmp && sudo mv /opt/toss-trader/trader.tmp /opt/toss-trader/trader'
```

롤백: `sudo mv /opt/toss-trader/trader.prev /opt/toss-trader/trader`.

- [ ] **Step 4: 첫 부팅 로그 검증 (다음 국장 개장일)**

VM 로그(`journalctl -u toss-trader`)에서 다음을 확인한다. 하나라도 어긋나면 원인을 파악하고 수정하기 전까지 그날은 결과를 검증 기간에 넣지 않는다.

- `스크리너 설정: …` 한 줄에 기본값이 그대로 찍혔는가.
- `사전 평가: 랭킹 N개(상위 [...])` — **장 시작 전 `Rankings(1d)`가 무엇을 돌려주는지**(어제 랭킹인지, NXT 프리마켓 반영인지, 빈 응답인지)를 이 줄로 확인해 설계 문서의 미확인 항목을 닫는다. 빈 응답이면 사전 평가는 0건이고 5분 지연 후 장중 갱신이 채운다(정상 동작).
- `… 셋업: 시가=… 목표가=…` 줄에서 **목표가가 시가보다 높은가**(9/30 버그 수정 검증).
- `활성 종목 편입: …` 이 개장 후 약 5분 뒤 처음 나오고, 이후 분 단위로 변동이 로그에 남는가.
- 429·5xx 오류가 반복되지 않는가(`분당 평가 예산` 줄이 자주 보이면 `EVAL_PER_MIN` 조정 검토).
- `Stock.leverageFactor`가 실제로 어떤 JSON 타입으로 오는지: 레버리지 ETF가 `평가 탈락(...): 레버리지/인버스`로 걸러지는지 로그에서 확인.
- 대시보드 `status.html`에 "활성 종목"/"탈락 종목" 표가 나오는가.

- [ ] **Step 5: 메모리 갱신**

`project-status.md`에 다음을 반영한다: 스크리너 라이브 구현·배포 여부와 날짜, 백테스트로 설정 확정 전이라는 점, 4주 카운트는 설정 확정 후 재시작한다는 점, 사전 랭킹 내용 확인 결과(Step 4).

---

## 이 계획의 범위 밖 (별도 계획)

- **스크리너 백테스트**: 과거 일자별 랭킹 재구성, 게이트/장중 확장/추격 상한/시작 지연 A/B 실험, 로컬 캔들 캐시. 이 결과로 기본값을 조정하고 설정을 확정한다(확정 시점이 4주 페이퍼 카운트 1일차).
- **미국장 부활**: 합산 잔고, 소수점 매수. 스크리너는 `market` 설정으로 미국에도 붙일 수 있게 만들었지만 US 세션 동작 검증은 별도 설계에서 한다.
- **`toOrder`의 매도 라벨 정리**: 모든 비-매수 액션이 "SELL"로 표시되는 기존 문제(`SkippedZeroShares` 등)는 이번 범위가 아니다.
