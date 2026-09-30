package snapshot

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
