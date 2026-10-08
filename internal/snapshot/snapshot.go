// Package snapshot은 SPEC.md 9절의 정적 대시보드 상태를 표현합니다.
package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"time"
)

// Position은 현재 보유 종목과 평가손익입니다.
type Position struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

// KillSwitchStatus는 신규 거래 중단 여부와 사유입니다.
type KillSwitchStatus struct {
	Halted bool   `json:"halted"`
	Reason string `json:"reason"`
}

// Order는 최근 주문의 표시용 상태입니다.
type Order struct {
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Quantity  float64   `json:"quantity"`
	Price     float64   `json:"price"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Snapshot의 금액은 동일 통화 기준입니다. DailyPnL은 비용을 포함한
// 실현+평가 손익이며 손실은 음수입니다. Seed는 계좌 실시간 잔고,
// DailyLossLimitPct는 5%일 때 0.05입니다.
type Snapshot struct {
	Positions         []Position       `json:"positions"`
	DailyPnL          float64          `json:"daily_pnl"`
	Seed              float64          `json:"seed"`
	Currency          string           `json:"currency,omitempty"`
	FXRate            float64          `json:"fx_rate,omitempty"` // 세션 내내 고정한 단위당 원화 환율
	DailyLossLimitPct float64          `json:"daily_loss_limit_pct"`
	KillSwitch        KillSwitchStatus `json:"kill_switch"`
	RecentOrders      []Order          `json:"recent_orders"`
	UpdatedAt         time.Time        `json:"updated_at"`
	Screener          *ScreenerStatus  `json:"screener,omitempty"`
	Version           string           `json:"version,omitempty"` // 이 스냅샷을 만든 trader 바이너리의 git 커밋
	Summary           *DaySummary      `json:"summary,omitempty"` // 마감 시점에만 채워지는 일일 요약
	Config            *EffectiveConfig `json:"config,omitempty"`  // 이 프로세스가 실제 적용 중인 설정(환경변수 적용 후)
}

// EffectiveConfig는 트레이더가 이번 세션에 실제로 적용 중인 설정입니다. 비율은 소수(0.02 = 2%)이고,
// 시간 값은 필드 이름의 단위를 따릅니다. 웹 시스템 화면에서 저장된 설정과 실행 중인 값을 비교하는 데 씁니다.
type EffectiveConfig struct {
	StopLossPct          float64 `json:"stop_loss_pct"`
	DailyLossLimitPct    float64 `json:"daily_loss_limit_pct"`
	K                    float64 `json:"k"`
	MAWindow             int     `json:"ma_window"`
	NoiseWindow          int     `json:"noise_window"`
	NoiseMin             float64 `json:"noise_min"`
	NoiseMax             float64 `json:"noise_max"`
	MinAffordable        int     `json:"min_affordable"`
	RankDepth            int     `json:"rank_depth"`
	ActiveCount          int     `json:"active_count"`
	ActiveKeepRank       int     `json:"active_keep_rank"`
	EvalPerMin           int     `json:"eval_per_min"`
	LazyExpand           bool    `json:"lazy_expand"`
	RankStartDelayMin    int     `json:"rank_start_delay_minutes"`
	RankRefreshSec       int     `json:"rank_refresh_seconds"`
	ChaseLimitPct        float64 `json:"chase_limit_pct"`
	PollIntervalSec      int     `json:"poll_interval_seconds"`
	HeartbeatIntervalSec int     `json:"heartbeat_interval_seconds"`
	EODBufferMin         int     `json:"eod_buffer_minutes"`
}

// DaySummary는 세션 마감 시점의 하루 결과와 계좌 누적 상태입니다. 금액은 모두 원화, 비율은 소수
// (0.1 = 10%)입니다. 마감 히스토리 파일 하나만 보고도 그날 성과와 낙폭 여유를 알 수 있게 합니다.
type DaySummary struct {
	Trades           int     `json:"trades"` // 그날 체결된 주문 수(매수+매도)
	DayPnLKRW        float64 `json:"day_pnl_krw"`
	AccountEquityKRW float64 `json:"account_equity_krw"`
	CumulativeReturn float64 `json:"cumulative_return"`
	CurrentDrawdown  float64 `json:"current_drawdown"`
	MaxDrawdown      float64 `json:"max_drawdown"`
	DrawdownLevel    string  `json:"drawdown_level"` // ok | warn | critical (report.DrawdownLevel)
}

// DailyLossProgress는 손실 한도 사용 비율을 반환합니다(1 = 100%).
// 한도 초과는 그대로 표시하며, 수익이나 유효하지 않은 입력은 0입니다.
func (s Snapshot) DailyLossProgress() float64 {
	if !finite(s.DailyPnL) || !finite(s.Seed) || !finite(s.DailyLossLimitPct) ||
		s.DailyPnL >= 0 || s.Seed <= 0 || s.DailyLossLimitPct <= 0 {
		return 0
	}
	limit := s.Seed * s.DailyLossLimitPct
	if limit <= 0 || !finite(limit) {
		return 0
	}
	return -s.DailyPnL / limit
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// RenderJSON은 스냅샷을 JSON으로 직렬화합니다.
func (s Snapshot) RenderJSON() ([]byte, error) {
	return json.Marshal(s)
}

// RenderHTML은 모든 문자열을 html/template으로 이스케이프합니다.
func (s Snapshot) RenderHTML() ([]byte, error) {
	var buf bytes.Buffer
	err := dashboard.Execute(&buf, struct {
		Snapshot
		LossPercent float64
	}{s, s.DailyLossProgress() * 100})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var dashboard = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"pct": func(v float64) string { return fmt.Sprintf("%.2f%%", v*100) },
}).Parse(`<!doctype html>
<html lang="ko">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>거래 상태</title></head>
<body>
<h1>거래 상태</h1>
<p>마지막 갱신 시각: {{.UpdatedAt.Format "2006-01-02T15:04:05Z07:00"}}</p>
{{if .Currency}}<p>금액 단위: {{.Currency}}{{if gt .FXRate 0.0}} (환율 {{printf "%.2f" .FXRate}}원){{end}}</p>{{end}}
<p>당일 실현+평가 손익(비용 포함): {{.DailyPnL}}</p>
<p>일일 손실 한도 대비 진행률: {{printf "%.2f" .LossPercent}}%</p>
<p>킬스위치: {{if .KillSwitch.Halted}}중단{{else}}정상{{end}} / {{.KillSwitch.Reason}}</p>
{{with .Summary}}
<h2>일일 요약</h2>
<p>체결 {{.Trades}}건 / 일 손익 {{printf "%.0f" .DayPnLKRW}}원 / 계좌 자산 {{printf "%.0f" .AccountEquityKRW}}원 (누적 {{pct .CumulativeReturn}})</p>
<p>고점 대비 현재 낙폭 {{pct .CurrentDrawdown}} / 최대 낙폭 {{pct .MaxDrawdown}} (검증 기준 10%)</p>
{{if or (eq .DrawdownLevel "warn") (eq .DrawdownLevel "critical")}}<p><strong>낙폭 경고({{.DrawdownLevel}}): 최대 낙폭이 검증 기준 10%에 가까워졌습니다</strong></p>{{end}}
{{end}}
<h2>현재 포지션</h2>
<table><thead><tr><th>종목</th><th>수량</th><th>평가손익</th></tr></thead><tbody>
{{range .Positions}}<tr><td>{{.Symbol}}</td><td>{{.Quantity}}</td><td>{{.UnrealizedPnL}}</td></tr>
{{else}}<tr><td colspan="3">보유 포지션 없음</td></tr>{{end}}
</tbody></table>
<h2>최근 주문</h2>
<table><thead><tr><th>시각</th><th>종목</th><th>방향</th><th>수량</th><th>가격</th><th>상태</th></tr></thead><tbody>
{{range .RecentOrders}}<tr><td>{{.CreatedAt.Format "2006-01-02T15:04:05Z07:00"}}</td><td>{{.Symbol}}</td><td>{{.Side}}</td><td>{{.Quantity}}</td><td>{{.Price}}</td><td>{{.Status}}</td></tr>
{{else}}<tr><td colspan="6">최근 주문 없음</td></tr>{{end}}
</tbody></table>
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
</body></html>
`))
