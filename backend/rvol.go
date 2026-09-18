package main

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// averageVolume 计算一组 K 线的平均成交量。
func averageVolume(klines []Kline) float64 {
	if len(klines) == 0 {
		return 0
	}

	var sum float64

	for _, kline := range klines {
		sum += volumeToFloat(kline.Volume)
	}

	return sum / float64(len(klines))
}

// computeRVOL 是 Ranking 与 Alert 共用的 RVOL 计算核心。
//
// currentVolume 为“当前 Kline 截至目前的成交量”，
// avg10 / avg30 分别为最近 10 / 30 根已完成 Kline 的平均成交量。
//
// 返回 (0, 0, false) 表示分母无效，无法计算。
func computeRVOL(
	currentVolume float64,
	avg10 float64,
	avg30 float64,
) (float64, float64, bool) {

	if currentVolume <= 0 {
		return 0, 0, false
	}

	if avg10 <= 0 || avg30 <= 0 {
		return 0, 0, false
	}

	return currentVolume / avg10,
		currentVolume / avg30,
		true
}

// calculateRVOLRecords 根据 REST 获取的历史 K 线，
// 计算最近 30 根已经完成的 RVOL。
//
// 为什么需要至少 61 根：
//
//	1 根 当前正在形成的 Kline
//	+ 30 根 需要计算 RVOL 的已完成 Kline
//	+ 30 根 最靠前那根 RVOL 需要的前置分母
//	= 61 根
func calculateRVOLRecords(
	klines []Kline,
) []RVOLRecord {

	if len(klines) < 61 {
		return nil
	}

	end := len(klines) - 1

	start := end - 30

	if start < 30 {
		return nil
	}

	records := make(
		[]RVOLRecord,
		0,
		30,
	)

	for i := start; i < end; i++ {

		currentVolume := volumeToFloat(
			klines[i].Volume,
		)

		avg10 := averageVolume(
			klines[i-10 : i],
		)

		avg30 := averageVolume(
			klines[i-30 : i],
		)

		rvol10, rvol30, ok := computeRVOL(
			currentVolume,
			avg10,
			avg30,
		)

		if !ok {
			continue
		}

		record := RVOLRecord{
			OpenTime: klines[i].OpenTime,

			Volume: currentVolume,

			RVOL10: rvol10,
			RVOL30: rvol30,
		}

		records = append(
			records,
			record,
		)
	}

	return records
}

// calculateNextRVOL 使用已经完成的历史 Kline，
// 计算“当前正在形成的 Kline”的实时 RVOL。
func calculateNextRVOL(
	history *RVOLHistory,
	currentVolume float64,
) (float64, float64, error) {

	if history == nil {
		return 0, 0, fmt.Errorf(
			"history 为 nil",
		)
	}

	avg10, ok := history.AverageVolume(10)

	if !ok {
		return 0, 0, fmt.Errorf(
			"历史数据不足10根，目前只有%d根",
			len(history.Records),
		)
	}

	avg30, ok := history.AverageVolume(30)

	if !ok {
		return 0, 0, fmt.Errorf(
			"历史数据不足30根，目前只有%d根",
			len(history.Records),
		)
	}

	rvol10, rvol30, ok := computeRVOL(
		currentVolume,
		avg10,
		avg30,
	)

	if !ok {
		return 0, 0, fmt.Errorf(
			"RVOL 计算失败：currentVolume=%.4f avg10=%.4f avg30=%.4f",
			currentVolume,
			avg10,
			avg30,
		)
	}

	return rvol10, rvol30, nil
}

func initializeHistory(
	symbol string,
) error {

	state, ok := states[symbol]

	if !ok {
		return fmt.Errorf(
			"找不到 SymbolState: %s",
			symbol,
		)
	}

	klines, err := getKlines(symbol)

	if err != nil {
		return err
	}

	if len(klines) < 61 {
		return fmt.Errorf(
			"%s 只获取到 %d 根 Kline，需要至少 61 根",
			symbol,
			len(klines),
		)
	}

	records := calculateRVOLRecords(
		klines,
	)

	if len(records) == 0 {
		return fmt.Errorf(
			"%s 无法计算历史 RVOL",
			symbol,
		)
	}

	var history RVOLHistory

	for _, record := range records {
		history.AddOrUpdate(record)
	}

	currentKline := klines[len(klines)-1]

	currentVolume :=
		volumeToFloat(
			currentKline.Volume,
		)

	rvol10, rvol30, err :=
		calculateNextRVOL(
			&history,
			currentVolume,
		)

	if err != nil {
		return err
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.History = history

	state.CurrentKlineOpenTime =
		currentKline.OpenTime

	state.CurrentVolume =
		currentVolume

	state.CurrentCycleMaxRVOL10 =
		rvol10

	state.CurrentCycleMaxRVOL30 =
		rvol30

	state.PreviousCycleMaxRVOL10 = 0
	state.PreviousCycleMaxRVOL30 = 0

	state.AlertedThisCycle = false

	return nil
}

// initializeAllHistory 并发初始化全部交易对。
//
// 汇总用 slog.Info，失败列表用 fmt（因为是给人看的）。
func initializeAllHistory(
	symbols []string,
) error {

	slog.Info("开始初始化历史数据", "count", len(symbols))

	var wg sync.WaitGroup

	errorsCh := make(
		chan error,
		len(symbols),
	)

	wg.Add(len(symbols))

	for _, symbol := range symbols {

		symbol := symbol

		go func() {
			defer wg.Done()

			err := initializeHistory(symbol)

			if err != nil {
				errorsCh <- fmt.Errorf(
					"%s: %w",
					symbol,
					err,
				)
			}
		}()
	}

	wg.Wait()

	close(errorsCh)

	// ========================================
	// 统计结果
	// ========================================

	failedSymbols := make([]string, 0)

	var firstErr error

	hasRateLimit := false

	for err := range errorsCh {

		failedSymbols = append(
			failedSymbols,
			err.Error(),
		)

		if firstErr == nil {
			firstErr = err
		}

		var rateLimitErr *RateLimitError

		if errors.As(
			err,
			&rateLimitErr,
		) {
			hasRateLimit = true
		}
	}

	failedCount := len(failedSymbols)
	successCount := len(symbols) - failedCount

	// ========================================
	// 输出汇总
	// ========================================

	if failedCount > 0 {
		slog.Warn("历史数据初始化完成（有失败）",
			"total", len(symbols),
			"success", successCount,
			"failed", failedCount,
			"rate_limit", hasRateLimit,
		)

		// 失败列表用 fmt（给人看）。
		fmt.Println()
		fmt.Println("失败交易对:")

		for _, failed := range failedSymbols {
			fmt.Printf("  - %s\n", failed)
		}

		fmt.Println()

		return fmt.Errorf(
			"初始化失败，共 %d 个交易对失败: %w",
			failedCount,
			firstErr,
		)
	}

	slog.Info("历史数据初始化完成",
		"total", len(symbols),
		"success", successCount,
	)

	return nil
}
