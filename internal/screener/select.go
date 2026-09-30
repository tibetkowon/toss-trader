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
