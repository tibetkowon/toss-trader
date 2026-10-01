package tossapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrice(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/prices" {
			t.Errorf("시세 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("symbols") != "AAPL" {
			t.Errorf("symbols 불일치: %q", r.URL.Query().Get("symbols"))
		}
		if r.Header.Get("X-Tossinvest-Account") != "" {
			t.Error("시세 조회에 계좌 헤더가 포함되었습니다")
		}
		w.Header().Set("X-RateLimit-Limit", "7")
		fmt.Fprint(w, `{"result":[{"symbol":"AAPL","timestamp":"2026-09-23T19:59:59.000+09:00","lastPrice":"123.45","currency":"USD"}]}`)
	})
	price, err := client.Price(context.Background(), "AAPL")
	if err != nil {
		t.Fatal(err)
	}
	if price == nil || price.Symbol != "AAPL" || price.LastPrice != "123.45" || price.Currency != "USD" || len(price.Raw) == 0 {
		t.Fatalf("현재가 또는 원문 불일치: %+v", price)
	}
	if limit := client.RateLimit("MARKET_DATA"); limit.Limit != 7 {
		t.Fatalf("시세 제한 그룹 불일치: %+v", limit)
	}
}

func TestPriceEmptySymbol(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "예상하지 않은 HTTP 요청", http.StatusBadRequest)
	})
	if _, err := client.Price(context.Background(), ""); err == nil {
		t.Fatal("빈 symbol을 허용했습니다")
	}
	if calls.Load() != 0 {
		t.Fatalf("빈 symbol로 HTTP 요청이 발생했습니다: %d", calls.Load())
	}
}

func TestCandles(t *testing.T) {
	const body = `{"candles":[{"timestamp":"2026-09-23T00:00:00.000+09:00","openPrice":"1","highPrice":"2","lowPrice":"0.5","closePrice":"1.5","volume":"100","currency":"KRW"}],"nextBefore":"2026-09-18T00:00:00.000+09:00"}`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/candles" {
			t.Errorf("캔들 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("symbol") != "AAPL" || query.Get("interval") != "1d" || query.Get("count") != "10" {
			t.Errorf("캔들 쿼리 불일치: %v", query)
		}
		if query.Has("before") {
			t.Error("빈 before가 쿼리에 포함되었습니다")
		}
		if r.Header.Get("X-Tossinvest-Account") != "" {
			t.Error("캔들 조회에 계좌 헤더가 포함되었습니다")
		}
		fmt.Fprint(w, body)
	})
	candles, nextBefore, err := client.Candles(context.Background(), "AAPL", "1d", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 1 {
		t.Fatalf("캔들 개수 불일치: %d", len(candles))
	}
	candle := candles[0]
	if candle.OpenPrice != "1" || candle.HighPrice != "2" || candle.LowPrice != "0.5" || candle.ClosePrice != "1.5" || candle.Volume != "100" {
		t.Fatalf("OHLCV 불일치: %+v", candle)
	}
	if len(candle.Raw) == 0 {
		t.Fatal("캔들 원문이 비어 있습니다")
	}
	if nextBefore != "2026-09-18T00:00:00.000+09:00" {
		t.Fatalf("nextBefore 불일치: %q", nextBefore)
	}
}

func TestCandlesWithBefore(t *testing.T) {
	const before = "2026-09-25T00:00:00+09:00"
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/candles" {
			t.Errorf("캔들 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("symbol") != "AAPL" || query.Get("interval") != "1d" || query.Get("count") != "10" || query.Get("before") != before {
			t.Errorf("캔들 쿼리 불일치: %v", query)
		}
		fmt.Fprint(w, `{"result":{"candles":[{"openPrice":"1","highPrice":"2","lowPrice":"0.5","closePrice":"1.5"}],"nextBefore":""}}`)
	})
	candles, nextBefore, err := client.Candles(context.Background(), "AAPL", "1d", 10, before)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 1 || candles[0].OpenPrice != "1" || candles[0].HighPrice != "2" || candles[0].LowPrice != "0.5" || candles[0].ClosePrice != "1.5" {
		t.Fatalf("캔들 응답 불일치: %+v", candles)
	}
	if nextBefore != "" {
		t.Fatalf("nextBefore 불일치: %q", nextBefore)
	}
}

func TestCandlesCountOutOfRange(t *testing.T) {
	for _, count := range []int{0, 201} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("잘못된 count로 HTTP 요청이 발생했습니다")
			})
			if _, _, err := client.Candles(context.Background(), "AAPL", "1d", count, ""); err == nil {
				t.Fatal("범위를 벗어난 count를 허용했습니다")
			}
		})
	}
}

func TestCandlesRateLimitGroupIndependent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			writeToken(w)
		case "/api/v1/candles":
			w.Header().Set("X-RateLimit-Limit", "2")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(5*time.Second).Unix(), 10))
			fmt.Fprint(w, `{"candles":[{"openPrice":"1","highPrice":"2","lowPrice":"0.5","closePrice":"1.5"}]}`)
		case "/api/v1/prices":
			fmt.Fprint(w, `{"result":[{"symbol":"AAPL","lastPrice":"1.5","currency":"USD"}]}`)
		default:
			t.Errorf("예상하지 않은 경로: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	client.now = func() time.Time { return now }
	var waits []time.Duration
	client.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	}
	if _, _, err := client.Candles(context.Background(), "AAPL", "1d", 10, ""); err != nil {
		t.Fatal(err)
	}
	if limit := client.RateLimit("MARKET_DATA_CHART"); limit.Limit != 2 || limit.Remaining != 0 || !limit.BlockedUntil.Equal(now.Add(5*time.Second)) {
		t.Fatalf("캔들 제한 그룹 불일치: %+v", limit)
	}
	if _, err := client.Price(context.Background(), "AAPL"); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 0 {
		t.Fatal("MARKET_DATA_CHART 제한이 MARKET_DATA 요청을 지연했습니다")
	}
	if _, _, err := client.Candles(context.Background(), "AAPL", "1d", 10, ""); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 5*time.Second {
		t.Fatalf("할당량 소진 대기: %v", waits)
	}
}

func TestMarketCalendar(t *testing.T) {
	const body = `{"today":{"date":"2026-09-23","integrated":{"preMarket":{"startTime":"2026-09-23T08:00:00.000+09:00","endTime":"2026-09-23T09:00:00.000+09:00"},"regularMarket":{"startTime":"2026-09-23T09:00:00.000+09:00","endTime":"2026-09-23T15:30:00.000+09:00"},"afterMarket":{"startTime":"2026-09-23T15:30:00.000+09:00","endTime":"2026-09-23T20:00:00.000+09:00"}}},"previousBusinessDay":{"date":"2026-09-22","integrated":null},"nextBusinessDay":{"date":"2026-09-24","integrated":null}}`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/market-calendar/KR" {
			t.Errorf("시장 캘린더 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("시장 캘린더 조회에 쿼리가 포함되었습니다: %s", r.URL.RawQuery)
		}
		if r.Header.Get("X-Tossinvest-Account") != "" {
			t.Error("시장 캘린더 조회에 계좌 헤더가 포함되었습니다")
		}
		w.Header().Set("X-RateLimit-Limit", "7")
		fmt.Fprint(w, body)
	})
	calendar, err := client.MarketCalendar(context.Background(), "KR")
	if err != nil {
		t.Fatal(err)
	}
	if calendar == nil || !calendar.IsOpenToday() || len(calendar.Raw) == 0 {
		t.Fatalf("개장 여부 또는 원문 불일치: %+v", calendar)
	}
	if calendar.Today.RegularSession().StartTime != "2026-09-23T09:00:00.000+09:00" {
		t.Fatalf("정규장 시작 시각 불일치: %+v", calendar.Today.RegularSession())
	}
	if limit := client.RateLimit("MARKET_INFO"); limit.Limit != 7 {
		t.Fatalf("시장 정보 제한 그룹 불일치: %+v", limit)
	}
}

func TestMarketCalendarClosedToday(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		fmt.Fprint(w, `{"today":{"date":"2026-09-27","integrated":null},"previousBusinessDay":{"date":"2026-09-25","integrated":null},"nextBusinessDay":{"date":"2026-09-28","integrated":null}}`)
	})
	calendar, err := client.MarketCalendar(context.Background(), "KR")
	if err != nil {
		t.Fatal(err)
	}
	if calendar.IsOpenToday() {
		t.Fatal("휴장일을 개장으로 판단했습니다")
	}
}

func TestRankings(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/rankings" {
			t.Errorf("랭킹 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("type") != "MARKET_TRADING_AMOUNT" || q.Get("duration") != "1mo" || q.Get("marketCountry") != "KR" {
			t.Errorf("랭킹 쿼리 불일치: %v", q)
		}
		fmt.Fprint(w, `{"rankedAt":"2026-09-23T20:17:38.787+09:00","rankings":[{"rank":1,"symbol":"000660","currency":"KRW","price":{"lastPrice":"1863000","basePrice":"1840000","changeRate":"0.0125"},"tradingVolume":"103629692","tradingAmount":"182438427324646"}]}`)
	})
	rankings, err := client.Rankings(context.Background(), "MARKET_TRADING_AMOUNT", "1mo", "KR")
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 1 || rankings[0].Rank != 1 || rankings[0].Symbol != "000660" || rankings[0].Price.LastPrice != "1863000" || rankings[0].TradingAmount != "182438427324646" || len(rankings[0].Raw) == 0 {
		t.Fatalf("랭킹 항목 불일치: %+v", rankings)
	}
}

func TestRankingsInvalidArgs(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("잘못된 인자로 HTTP 요청이 발생했습니다")
	})
	if _, err := client.Rankings(context.Background(), "", "1mo", "KR"); err == nil {
		t.Fatal("빈 rankingType을 허용했습니다")
	}
	if _, err := client.Rankings(context.Background(), "MARKET_TRADING_AMOUNT", "1mo", "XX"); err == nil {
		t.Fatal("잘못된 marketCountry를 허용했습니다")
	}
}

func TestStockWarnings(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/stocks/005930/warnings" {
			t.Errorf("유의사항 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"result":[]}`)
	})
	warnings, err := client.StockWarnings(context.Background(), "005930")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("유의사항 없음을 잘못 파싱했습니다: %+v", warnings)
	}
}

func TestStockWarningsEmptySymbol(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("빈 symbol로 HTTP 요청이 발생했습니다")
	})
	if _, err := client.StockWarnings(context.Background(), ""); err == nil {
		t.Fatal("빈 symbol을 허용했습니다")
	}
}

func TestStocks(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/stocks" {
			t.Errorf("종목 정보 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("symbols") != "005930" {
			t.Errorf("symbols 불일치: %q", r.URL.Query().Get("symbols"))
		}
		fmt.Fprint(w, `{"result":[{"symbol":"005930","name":"삼성전자","englishName":"SamsungElec","market":"KOSPI","securityType":"STOCK","status":"ACTIVE","currency":"KRW","sharesOutstanding":"5846278608","koreanMarketDetail":{"liquidationTrading":false,"nxtSupported":true,"krxTradingSuspended":false,"nxtTradingSuspended":false}}]}`)
	})
	stocks, err := client.Stocks(context.Background(), "005930")
	if err != nil {
		t.Fatal(err)
	}
	if len(stocks) != 1 || stocks[0].Name != "삼성전자" || stocks[0].SharesOutstanding != "5846278608" || stocks[0].KoreanMarketDetail == nil || stocks[0].KoreanMarketDetail.LiquidationTrading || len(stocks[0].Raw) == 0 {
		t.Fatalf("종목 정보 불일치: %+v", stocks)
	}
}

func TestStocksNoSymbols(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("symbols 없이 HTTP 요청이 발생했습니다")
	})
	if _, err := client.Stocks(context.Background()); err == nil {
		t.Fatal("빈 symbols를 허용했습니다")
	}
}

func TestCommissions(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/commissions" {
			t.Errorf("수수료 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Tossinvest-Account") != "1" {
			t.Error("계좌 헤더가 전달되지 않았습니다")
		}
		fmt.Fprint(w, `{"result":[{"marketCountry":"KR","commissionRate":"0.00015","startDate":"2021-01-01","endDate":"9999-12-31"},{"marketCountry":"US","commissionRate":"0.001","startDate":null,"endDate":"2026-09-28"}]}`)
	})
	commissions, err := client.Commissions(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(commissions) != 2 || commissions[0].MarketCountry != "KR" || commissions[0].CommissionRate != "0.00015" {
		t.Fatalf("수수료 목록 불일치: %+v", commissions)
	}
}

func TestCommissionsEmptyAccount(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("빈 accountSeq로 HTTP 요청이 발생했습니다")
	})
	if _, err := client.Commissions(context.Background(), ""); err == nil {
		t.Fatal("빈 accountSeq를 허용했습니다")
	}
}

// TestMarketCalendarUSShape uses the real US response shape confirmed
// live 2026-09-28: unlike KR, there is no "integrated" wrapper at all —
// preMarket/regularMarket/afterMarket (plus an unexplained "dayMarket")
// sit directly on the day object. IsOpenToday()/RegularSession() must
// handle both shapes (see MarketDay's doc comment).
func TestMarketCalendarUSShape(t *testing.T) {
	const body = `{"today":{"date":"2026-09-28","dayMarket":{"startTime":"2026-09-28T09:00:00.000+09:00","endTime":"2026-09-28T17:00:00.000+09:00"},"preMarket":{"startTime":"2026-09-28T17:00:00.000+09:00","endTime":"2026-09-28T22:30:00.000+09:00"},"regularMarket":{"startTime":"2026-09-28T22:30:00.000+09:00","endTime":"2026-09-29T05:00:00.000+09:00"},"afterMarket":{"startTime":"2026-09-29T05:00:00.000+09:00","endTime":"2026-09-29T08:50:00.000+09:00"}},"previousBusinessDay":{"date":"2026-09-25"},"nextBusinessDay":{"date":"2026-09-29"}}`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.URL.Path != "/api/v1/market-calendar/US" {
			t.Errorf("경로 불일치: %s", r.URL.Path)
		}
		fmt.Fprint(w, body)
	})
	calendar, err := client.MarketCalendar(context.Background(), "US")
	if err != nil {
		t.Fatal(err)
	}
	if !calendar.IsOpenToday() {
		t.Fatal("US 개장일을 휴장으로 잘못 판단했습니다(integrated 래퍼가 없는 실제 구조)")
	}
	regular := calendar.Today.RegularSession()
	if regular == nil || regular.StartTime != "2026-09-28T22:30:00.000+09:00" || regular.EndTime != "2026-09-29T05:00:00.000+09:00" {
		t.Fatalf("정규장 시간 불일치: %+v", regular)
	}
	if calendar.Today.Integrated != nil {
		t.Fatalf("US 응답에는 integrated가 없어야 합니다: %+v", calendar.Today.Integrated)
	}
}

func TestMarketCalendarInvalidMarket(t *testing.T) {
	for _, market := range []string{"XX", ""} {
		t.Run(market, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("잘못된 market으로 HTTP 요청이 발생했습니다")
			})
			if _, err := client.MarketCalendar(context.Background(), market); err == nil {
				t.Fatal("잘못된 market을 허용했습니다")
			}
		})
	}
}

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

func TestPricesBatchesIntoOneCall(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		calls.Add(1)
		if got := r.URL.Query().Get("symbols"); got != "A,B,C" {
			t.Errorf("symbols 불일치: %q", got)
		}
		fmt.Fprint(w, `{"result":[{"symbol":"A","timestamp":"t","lastPrice":"1"},{"symbol":"C","timestamp":"t","lastPrice":"3"}]}`)
	})
	got, err := client.Prices(context.Background(), "A", "B", "C")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(got) != 2 || got["A"].LastPrice != "1" || got["C"].LastPrice != "3" || len(got["A"].Raw) == 0 {
		t.Fatalf("calls=%d got=%+v", calls.Load(), got)
	}
	if _, ok := got["B"]; ok {
		t.Error("응답에 없는 종목은 결과에서 빠져야 합니다")
	}
}

func TestPricesRejectsBadInput(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("HTTP 요청이 발생했습니다: %s", r.URL)
	})
	tooMany := make([]string, MaxPricesPerCall+1)
	for i := range tooMany {
		tooMany[i] = "S" + strconv.Itoa(i)
	}
	for name, symbols := range map[string][]string{"none": nil, "too many": tooMany, "blank": {"A", " "}, "comma": {"A,B"}} {
		if _, err := client.Prices(context.Background(), symbols...); err == nil {
			t.Errorf("%s: 잘못된 입력을 허용했습니다", name)
		}
	}
}
