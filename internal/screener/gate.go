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
