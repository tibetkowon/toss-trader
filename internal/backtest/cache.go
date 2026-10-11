package backtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

const DefaultCacheDir = ".cache/backtest"

// Cache는 종목·간격별 JSON을 원자적으로 교체합니다.
type Cache struct{ Dir string }

type SymbolData struct {
	Daily    []tossapi.Candle
	Minutes  []tossapi.Candle
	Stocks   []tossapi.Stock
	Warnings []json.RawMessage
}

func (c Cache) path(market, symbol, kind string) (string, error) {
	if market != "KR" && market != "US" {
		return "", fmt.Errorf("잘못된 시장: %q", market)
	}
	if symbol == "" || symbol == "." || symbol == ".." || filepath.Base(symbol) != symbol {
		return "", fmt.Errorf("잘못된 심볼: %q", symbol)
	}
	dir := c.Dir
	if dir == "" {
		dir = DefaultCacheDir
	}
	return filepath.Join(dir, market, symbol, kind+".json"), nil
}
func (c Cache) load(market, symbol, kind string, dst any) error {
	p, err := c.path(market, symbol, kind)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}
func (c Cache) save(market, symbol, kind string, value any) error {
	p, err := c.path(market, symbol, kind)
	if err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
func (c Cache) LoadUniverse(market string) ([]tossapi.RankingItem, error) {
	var v []tossapi.RankingItem
	err := c.load(market, "_universe", "1mo", &v)
	return v, err
}
func (c Cache) SaveUniverse(market string, v []tossapi.RankingItem) error {
	return c.save(market, "_universe", "1mo", v)
}

// Raw는 tossapi.Stock의 JSON에서 제외되므로 판정에 필요한 원문을 함께 보존합니다.
type cachedStocks struct {
	Stocks []tossapi.Stock
	Raw    []json.RawMessage
}

func (c Cache) saveStocks(market, symbol string, stocks []tossapi.Stock) error {
	v := cachedStocks{Stocks: stocks}
	for _, s := range stocks {
		v.Raw = append(v.Raw, s.Raw)
	}
	return c.save(market, symbol, "stocks", v)
}
func (c Cache) loadStocks(market, symbol string) ([]tossapi.Stock, error) {
	var v cachedStocks
	if err := c.load(market, symbol, "stocks", &v); err != nil {
		return nil, err
	}
	if len(v.Raw) != len(v.Stocks) {
		return nil, fmt.Errorf("%s: 종목 원문 누락", symbol)
	}
	for i := range v.Stocks {
		v.Stocks[i].Raw = v.Raw[i]
	}
	return v.Stocks, nil
}
func (c Cache) SaveSymbol(market, symbol string, d SymbolData) error {
	if err := c.save(market, symbol, "1d", d.Daily); err != nil {
		return err
	}
	if err := c.save(market, symbol, "1m", d.Minutes); err != nil {
		return err
	}
	if err := c.saveStocks(market, symbol, d.Stocks); err != nil {
		return err
	}
	return c.save(market, symbol, "warnings", d.Warnings)
}
func (c Cache) LoadSymbol(market, symbol string) (SymbolData, error) {
	var d SymbolData
	if err := c.load(market, symbol, "1d", &d.Daily); err != nil {
		return d, err
	}
	if err := c.load(market, symbol, "1m", &d.Minutes); err != nil {
		return d, err
	}
	var err error
	if d.Stocks, err = c.loadStocks(market, symbol); err != nil {
		return d, err
	}
	err = c.load(market, symbol, "warnings", &d.Warnings)
	return d, err
}
