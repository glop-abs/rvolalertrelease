package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Binance REST API 地址。
const binanceBaseURL = "https://fapi.binance.com"

// RateLimitError 表示 Binance REST API 返回 HTTP 429。
type RateLimitError struct {
	Symbol string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf(
		"%s 触发 Binance REST API 限流（HTTP 429）",
		e.Symbol,
	)
}

// getKlines 获取指定交易对的历史 K 线。
//
// 当前固定：
//
// symbol   = 指定交易对
// interval = 15m
// limit    = 61
func getKlines(symbol string) ([]Kline, error) {
	url := fmt.Sprintf(
		"%s/fapi/v1/klines?symbol=%s&interval=15m&limit=61",
		binanceBaseURL,
		symbol,
	)

	var lastErr error

	// 普通网络错误最多重试 3 次。
	// HTTP 429 不在这里重试。
	for attempt := 1; attempt <= 3; attempt++ {
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get(url)

		if err != nil {
			lastErr = err

			if attempt < 3 {
				slog.Warn("Kline 请求失败，重试",
					"symbol", symbol,
					"attempt", attempt,
					"err", err,
				)

				time.Sleep(2 * time.Second)
			}

			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err

			if attempt < 3 {
				slog.Warn("Kline 响应读取失败，重试",
					"symbol", symbol,
					"attempt", attempt,
					"err", err,
				)

				time.Sleep(2 * time.Second)
			}

			continue
		}

		// ========================================
		// HTTP 429
		// ========================================
		//
		// 不进行普通重试。
		// 直接交给上层处理。
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &RateLimitError{
				Symbol: symbol,
			}
		}

		// ========================================
		// 其他非 200 状态
		// ========================================

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf(
				"HTTP 状态码: %d，响应: %s",
				resp.StatusCode,
				string(body),
			)

			if attempt < 3 {
				slog.Warn("Kline HTTP 非 200，重试",
					"symbol", symbol,
					"attempt", attempt,
					"status", resp.StatusCode,
				)

				time.Sleep(2 * time.Second)
			}

			continue
		}

		// ========================================
		// JSON 解析
		// ========================================

		var raw [][]interface{}

		if err := json.Unmarshal(body, &raw); err != nil {
			lastErr = err

			if attempt < 3 {
				slog.Warn("Kline JSON 解析失败，重试",
					"symbol", symbol,
					"attempt", attempt,
					"err", err,
				)

				time.Sleep(2 * time.Second)
			}

			continue
		}

		klines := make([]Kline, 0, len(raw))

		for _, item := range raw {
			if len(item) < 12 {
				continue
			}

			kline := Kline{
				OpenTime: int64(item[0].(float64)),

				Open:   item[1].(string),
				High:   item[2].(string),
				Low:    item[3].(string),
				Close:  item[4].(string),
				Volume: item[5].(string),

				CloseTime: int64(item[6].(float64)),

				QuoteVolume: item[7].(string),

				TradeCount: int(item[8].(float64)),

				TakerBuyVolume:   item[9].(string),
				TakerBuyQuoteVol: item[10].(string),
			}

			klines = append(
				klines,
				kline,
			)
		}

		return klines, nil
	}

	return nil, fmt.Errorf(
		"获取 %s Kline 失败: %w",
		symbol,
		lastErr,
	)
}

// volumeToFloat 将 Binance 的成交量字符串转换成 float64。
func volumeToFloat(volume string) float64 {
	value, err := strconv.ParseFloat(volume, 64)

	if err != nil {
		return 0
	}

	return value
}
