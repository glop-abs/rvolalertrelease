package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	exchangeInfoURL = "https://fapi.binance.com/fapi/v1/exchangeInfo"
	klineURL        = "https://fapi.binance.com/fapi/v1/klines"

	// 每个交易对获取最近61根15分钟K线
	klineLimit = 61
)

type ExchangeInfo struct {
	Symbols []Symbol `json:"symbols"`
}

type Symbol struct {
	Symbol       string `json:"symbol"`
	Status       string `json:"status"`
	ContractType string `json:"contractType"`
	QuoteAsset   string `json:"quoteAsset"`
}

type Result struct {
	Symbol  string
	Success bool
	Status  int
	Klines  int
	Elapsed time.Duration
	Weight  string
	RawBody string
	Error   error
}

func main() {

	client := &http.Client{
		Timeout: 10 * time.Second,

		// 允许大量连接复用/并发连接
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			MaxConnsPerHost:     1000,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	fmt.Println("========================================")
	fmt.Println(" Binance 15m Kline REST Rate Limit Test")
	fmt.Println("========================================")
	fmt.Println()

	// --------------------------------------------------
	// 1. 获取完整交易对列表
	// --------------------------------------------------

	fmt.Println("正在获取完整交易对列表...")

	symbols, err := getSymbols(client)
	if err != nil {
		fmt.Println("获取交易对失败:", err)
		return
	}

	maxCount := len(symbols)

	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("当前可测试最大交易对数量：%d\n", maxCount)
	fmt.Println("========================================")
	fmt.Println()

	// --------------------------------------------------
	// 2. 输入测试数量
	// --------------------------------------------------

	fmt.Printf("请输入本次测试数量（1-%d）：", maxCount)

	var input string
	fmt.Scanln(&input)

	count, err := strconv.Atoi(input)
	if err != nil || count <= 0 {
		fmt.Println("输入无效，程序退出。")
		return
	}

	if count > maxCount {
		fmt.Printf(
			"输入数量 %d 超过最大值 %d，自动使用最大值。\n",
			count,
			maxCount,
		)

		count = maxCount
	}

	// 只取前 count 个
	testSymbols := symbols[:count]

	// 用第一个测试交易对作为完整响应样本
	sampleSymbol := testSymbols[0].Symbol

	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("本次测试数量：%d\n", count)
	fmt.Printf("并发数量：%d\n", count)
	fmt.Printf("Kline：15m\n")
	fmt.Printf("Limit：%d\n", klineLimit)
	fmt.Printf("完整响应样本：%s\n", sampleSymbol)
	fmt.Println("========================================")
	fmt.Println()

	// --------------------------------------------------
	// 3. 并发测试
	// --------------------------------------------------

	start := time.Now()

	results := make(chan Result, count)

	var wg sync.WaitGroup

	wg.Add(count)

	for _, symbol := range testSymbols {

		symbol := symbol

		go func() {
			defer wg.Done()

			result := requestKline(client, symbol.Symbol)

			results <- result
		}()
	}

	// 等待所有请求完成
	wg.Wait()

	close(results)

	totalTime := time.Since(start)

	// --------------------------------------------------
	// 4. 统计结果
	// --------------------------------------------------

	success := 0
	failed := 0
	rateLimit429 := 0

	var maxElapsed time.Duration
	var lastWeight string

	for result := range results {

		if result.Success {
			success++
		} else {
			failed++
		}

		if result.Status == http.StatusTooManyRequests {
			rateLimit429++
		}

		if result.Elapsed > maxElapsed {
			maxElapsed = result.Elapsed
		}

		if result.Weight != "" {
			lastWeight = result.Weight
		}

		fmt.Printf(
			"%-15s HTTP=%d Kline=%d Time=%v Weight=%s\n",
			result.Symbol,
			result.Status,
			result.Klines,
			result.Elapsed,
			result.Weight,
		)

		// 只打印一个请求的完整原始 REST 响应
		if result.RawBody != "" && result.Symbol == sampleSymbol {
			fmt.Println()
			fmt.Println("========================================")
			fmt.Printf("%s 完整 REST 响应\n", result.Symbol)
			fmt.Println("========================================")
			fmt.Println(result.RawBody)
			fmt.Println("========================================")
			fmt.Println()
		}
	}

	// --------------------------------------------------
	// 5. 最终结果
	// --------------------------------------------------

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("测试结果")
	fmt.Println("========================================")

	fmt.Printf("最大可测试数量：%d\n", maxCount)
	fmt.Printf("本次测试数量：%d\n", count)
	fmt.Printf("并发数量：%d\n", count)

	fmt.Println()

	fmt.Printf("成功：%d\n", success)
	fmt.Printf("失败：%d\n", failed)
	fmt.Printf("HTTP 429：%d\n", rateLimit429)

	fmt.Println()

	fmt.Printf("总耗时：%v\n", totalTime)
	fmt.Printf("最慢请求：%v\n", maxElapsed)

	if totalTime > 0 {
		fmt.Printf(
			"实际吞吐：%.2f 请求/秒\n",
			float64(success)/totalTime.Seconds(),
		)
	}

	fmt.Printf(
		"最后记录的 REQUEST_WEIGHT-1M：%s\n",
		lastWeight,
	)

	fmt.Println("========================================")
}

func getSymbols(client *http.Client) ([]Symbol, error) {

	resp, err := client.Get(exchangeInfoURL)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"exchangeInfo HTTP %d",
			resp.StatusCode,
		)
	}

	var info ExchangeInfo

	err = json.NewDecoder(resp.Body).Decode(&info)
	if err != nil {
		return nil, err
	}

	var result []Symbol

	for _, symbol := range info.Symbols {

		if symbol.Status != "TRADING" {
			continue
		}

		if symbol.ContractType != "PERPETUAL" {
			continue
		}

		if symbol.QuoteAsset != "USDT" {
			continue
		}

		result = append(result, symbol)
	}

	return result, nil
}

func requestKline(
	client *http.Client,
	symbol string,
) Result {

	start := time.Now()

	url := fmt.Sprintf(
		"%s?symbol=%s&interval=15m&limit=%d",
		klineURL,
		symbol,
		klineLimit,
	)

	resp, err := client.Get(url)

	elapsed := time.Since(start)

	if err != nil {
		return Result{
			Symbol:  symbol,
			Elapsed: elapsed,
			Error:   err,
		}
	}

	defer resp.Body.Close()

	weight := resp.Header.Get("X-MBX-USED-WEIGHT-1M")

	body, err := io.ReadAll(resp.Body)

	if err != nil {
		return Result{
			Symbol:  symbol,
			Status:  resp.StatusCode,
			Elapsed: elapsed,
			Weight:  weight,
			Error:   err,
		}
	}

	if resp.StatusCode != http.StatusOK {

		return Result{
			Symbol:  symbol,
			Status:  resp.StatusCode,
			Elapsed: elapsed,
			Weight:  weight,
			RawBody: string(body),
		}
	}

	var klines [][]interface{}

	err = json.Unmarshal(body, &klines)

	if err != nil {
		return Result{
			Symbol:  symbol,
			Status:  resp.StatusCode,
			Elapsed: elapsed,
			Weight:  weight,
			RawBody: string(body),
			Error:   err,
		}
	}

	return Result{
		Symbol:  symbol,
		Success: true,
		Status:  resp.StatusCode,
		Klines:  len(klines),
		Elapsed: elapsed,
		Weight:  weight,
		RawBody: string(body),
	}
}
