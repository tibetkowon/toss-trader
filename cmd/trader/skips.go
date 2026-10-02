package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func formatShares(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// skipTracker는 0주 스킵(현금이 1주 가격에 못 미침)을 종목별로 집계합니다. 신호가 살아 있는 동안
// 매 틱 반복되므로 첫 발생만 로그로 남기고, 상태 저장·스냅샷 업로드·최근 주문 목록에서는 뺍니다.
type skipTracker struct {
	stats map[string]*skipStat
}

type skipStat struct {
	count      int
	first, min float64
	max        float64
}

func newSkipTracker() *skipTracker { return &skipTracker{stats: map[string]*skipStat{}} }

func (t *skipTracker) filter(actions []simulator.Action, cash float64) []simulator.Action {
	kept := actions[:0:0]
	for _, a := range actions {
		if a.Type != simulator.SkippedZeroShares {
			kept = append(kept, a)
			continue
		}
		st, seen := t.stats[a.Symbol]
		if !seen {
			st = &skipStat{first: a.Price, min: a.Price, max: a.Price}
			t.stats[a.Symbol] = st
			log.Printf("0주 스킵: %s @ %.2f — 현금 %.2f로 1주도 못 삽니다(이후 같은 종목 스킵은 집계만 하고 마감 때 요약합니다)", a.Symbol, a.Price, cash)
		}
		st.count++
		st.min, st.max = min(st.min, a.Price), max(st.max, a.Price)
	}
	return kept
}

func (t *skipTracker) summary() []string {
	symbols := make([]string, 0, len(t.stats))
	for s := range t.stats {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	lines := make([]string, 0, len(symbols))
	for _, s := range symbols {
		st := t.stats[s]
		lines = append(lines, fmt.Sprintf("0주 스킵 요약: %s %d회 (최초 %.2f, 범위 %.2f~%.2f)", s, st.count, st.first, st.min, st.max))
	}
	return lines
}
