// 옛 고정 워치리스트(2026-09-27 선정, SPEC.md 3.3 v15에서 폐기). 6.2 백테스트 결과의 근거이자
// cmd/backtest의 기본 종목 목록으로만 보존합니다. 라이브 거래는 internal/screener를 씁니다.
package main

// WatchlistEntry is one fixed-watchlist symbol with the metadata that
// justified including it, so the list is auditable without re-running the
// screening.
type WatchlistEntry struct {
	Symbol string
	Name   string
	// NoiseRatioPercent is the mean daily (high-low)/open over the trailing
	// 20 trading days at selection time, as a percentage.
	NoiseRatioPercent float64
	// PriceAtSelection is the last close (KRW) observed at selection time.
	// Several of these exceed a ~100,000 KRW seed today; SPEC.md 4.1's seed
	// is the live account balance and is expected to grow, and SPEC.md 8's
	// "0주면 스킵" rule means an unaffordable symbol's breakout signal is
	// just skipped that day, not an error — no watchlist change is needed
	// as the seed grows into these prices.
	PriceAtSelection int
}

// Watchlist is the fixed 1단계 워치리스트 (SPEC.md 3.3), selected 2026-09-27
// against the live Toss Securities API: top-30 by 1개월 거래대금
// (GET /api/v1/rankings, type=MARKET_TRADING_AMOUNT), leveraged/inverse ETFs
// excluded (Stock.leverageFactor not in {"", "1"}), zero 유의사항
// (GET /api/v1/stocks/{symbol}/warnings) and not KRX-suspended/정리매매
// (Stock.KoreanMarketDetail), then chosen for a mix of noise ratios so the
// list isn't dominated by either near-zero-volatility index ETFs or
// currently-unaffordable large caps. See SPEC.md 3.3/11 for the full
// methodology and its caveats (top-30-by-liquidity sample only, one-time
// selection — 2단계에서 자동화 예정).
var Watchlist = []WatchlistEntry{
	{Symbol: "005930", Name: "삼성전자", NoiseRatioPercent: 3.49, PriceAtSelection: 286500},
	{Symbol: "000660", Name: "SK하이닉스", NoiseRatioPercent: 4.54, PriceAtSelection: 1863000},
	{Symbol: "066570", Name: "LG전자", NoiseRatioPercent: 4.99, PriceAtSelection: 211000},
	{Symbol: "005380", Name: "현대차", NoiseRatioPercent: 3.05, PriceAtSelection: 357000},
	{Symbol: "034020", Name: "두산에너빌리티", NoiseRatioPercent: 5.70, PriceAtSelection: 82700},
	{Symbol: "396500", Name: "TIGER 반도체TOP10", NoiseRatioPercent: 3.30, PriceAtSelection: 39395},
	{Symbol: "229200", Name: "KODEX 코스닥150", NoiseRatioPercent: 2.95, PriceAtSelection: 14150},
	{Symbol: "069500", Name: "KODEX 200", NoiseRatioPercent: 2.50, PriceAtSelection: 113145},
}
