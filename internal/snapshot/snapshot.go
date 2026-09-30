// Package snapshot은 SPEC.md 9절의 정적 대시보드 상태를 표현합니다.
package snapshot

import (
	"bytes"
	"encoding/json"
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
	DailyLossLimitPct float64          `json:"daily_loss_limit_pct"`
	KillSwitch        KillSwitchStatus `json:"kill_switch"`
	RecentOrders      []Order          `json:"recent_orders"`
	UpdatedAt         time.Time        `json:"updated_at"`
	Screener          *ScreenerStatus  `json:"screener,omitempty"`
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

var dashboard = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="ko">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>거래 상태</title></head>
<body>
<h1>거래 상태</h1>
<p>마지막 갱신 시각: {{.UpdatedAt.Format "2006-01-02T15:04:05Z07:00"}}</p>
<p>당일 실현+평가 손익(비용 포함): {{.DailyPnL}}</p>
<p>일일 손실 한도 대비 진행률: {{printf "%.2f" .LossPercent}}%</p>
<p>킬스위치: {{if .KillSwitch.Halted}}중단{{else}}정상{{end}} / {{.KillSwitch.Reason}}</p>
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
