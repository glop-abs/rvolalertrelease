package main

import (
	"log/slog"
	"strconv"
	"sync"
)

// Ticker24h 是我们关心的 24h ticker 字段。
type Ticker24h struct {
	Symbol             string
	PriceChangePercent float64
	HasData            bool
}

// tickerCache 缓存全市场 24h ticker。
//
// key = symbol
//
// 只保留每个 symbol 最新的一份数据。
var tickerCache = struct {
	mu sync.RWMutex
	m  map[string]Ticker24h
}{
	m: make(map[string]Ticker24h),
}

// updateTickerCache 更新 24h ticker 缓存。
//
// 这里使用“合并”而不是“覆盖”。
//
// 原因：即使某次 WebSocket 消息只包含部分 ticker，
// 也不能因为这次消息没有某个 symbol，
// 就把该 symbol 原来的有效数据删除。
func updateTickerCache(tickers map[string]Ticker24h) {

	tickerCache.mu.Lock()
	defer tickerCache.mu.Unlock()

	for symbol, ticker := range tickers {
		tickerCache.m[symbol] = ticker
	}

	slog.Debug(
		"24h ticker 缓存更新",
		"received",
		len(tickers),
		"cached",
		len(tickerCache.m),
	)
}

// getPriceChangePercent 返回某币的 24h 涨跌幅（%）。
//
// 返回 (0, false) 表示没有数据。
func getPriceChangePercent(symbol string) (float64, bool) {

	tickerCache.mu.RLock()

	t, ok := tickerCache.m[symbol]

	tickerCache.mu.RUnlock()

	if !ok || !t.HasData {
		return 0, false
	}

	return t.PriceChangePercent, true
}

// parseTickerArray 解析 !ticker@arr 推送的数组。
//
// Binance 的 priceChangePercent 是字符串，需要手动转 float。
func parseTickerArray(
	raw []map[string]interface{},
) map[string]Ticker24h {

	result := make(
		map[string]Ticker24h,
		len(raw),
	)

	for _, item := range raw {

		symbol, _ :=
			item["s"].(string)

		if symbol == "" {
			continue
		}

		// "P" = priceChangePercent，字符串。
		pStr, _ :=
			item["P"].(string)

		if pStr == "" {

			result[symbol] = Ticker24h{
				Symbol:  symbol,
				HasData: false,
			}

			continue
		}

		v, err :=
			strconv.ParseFloat(
				pStr,
				64,
			)

		if err != nil {

			result[symbol] = Ticker24h{
				Symbol:  symbol,
				HasData: false,
			}

			continue
		}

		result[symbol] = Ticker24h{
			Symbol:             symbol,
			PriceChangePercent: v,
			HasData:            true,
		}
	}

	return result
}
