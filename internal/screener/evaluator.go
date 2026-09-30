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
