package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// SymbolInfo 是 Binance ExchangeInfo 中我们关心的字段。
type SymbolInfo struct {
	Symbol       string `json:"symbol"`
	Status       string `json:"status"`
	ContractType string `json:"contractType"`
	QuoteAsset   string `json:"quoteAsset"`
}

// ExchangeInfoResponse 是 Binance ExchangeInfo 返回结构。
type ExchangeInfoResponse struct {
	Symbols []SymbolInfo `json:"symbols"`
}

// getSymbols 获取符合条件的 USDT 永续合约交易对。
//
// 当前过滤条件：
//
// Status       = TRADING
// ContractType = PERPETUAL
// QuoteAsset   = USDT
func getSymbols() ([]string, error) {
	url := binanceBaseURL + "/fapi/v1/exchangeInfo"

	client := &http.Client{}

	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		slog.Info("请求 Binance ExchangeInfo",
			"attempt", attempt,
			"max", 3,
		)

		resp, err := client.Get(url)

		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf(
				"HTTP 状态码: %d",
				resp.StatusCode,
			)

			continue
		}

		var data ExchangeInfoResponse

		if err := json.Unmarshal(
			body,
			&data,
		); err != nil {
			lastErr = err
			continue
		}

		symbols := make(
			[]string,
			0,
		)

		for _, symbol := range data.Symbols {
			if symbol.Status != "TRADING" {
				continue
			}

			if symbol.ContractType != "PERPETUAL" {
				continue
			}

			if symbol.QuoteAsset != "USDT" {
				continue
			}

			symbols = append(
				symbols,
				symbol.Symbol,
			)
		}

		return symbols, nil
	}

	return nil, fmt.Errorf(
		"获取 ExchangeInfo 失败: %w",
		lastErr,
	)
}
