package main

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// startRVOLRanking 启动全市场 RVOL Ranking。
func startRVOLRanking() {

	interval :=
		config.RankingInterval()

	ticker :=
		time.NewTicker(interval)

	defer ticker.Stop()

	fmt.Printf(
		"RVOL 排名刷新间隔: %s\n",
		interval,
	)

	calculateAndPrintTopRVOL()

	for range ticker.C {

		calculateAndPrintTopRVOL()
	}
}

// calculateAndPrintTopRVOL 打印当前 Ranking Top 10。
func calculateAndPrintTopRVOL() {

	results :=
		calculateAllRVOL()

	if len(results) == 0 {

		fmt.Println()
		fmt.Println(
			"当前没有可用 RVOL 数据",
		)

		return
	}

	sort.Slice(
		results,
		func(i, j int) bool {

			if results[i].HighRVOLRate30 !=
				results[j].HighRVOLRate30 {

				return results[i].HighRVOLRate30 >
					results[j].HighRVOLRate30
			}

			if results[i].HighRVOLRate15 !=
				results[j].HighRVOLRate15 {

				return results[i].HighRVOLRate15 >
					results[j].HighRVOLRate15
			}

			return results[i].RankRVOL >
				results[j].RankRVOL
		},
	)

	topN := 10

	if len(results) < topN {
		topN = len(results)
	}

	fmt.Println()
	fmt.Println(
		"================================",
	)

	fmt.Printf(
		"RVOL Ranking Top %d | %s\n",
		topN,
		time.Now().Format(
			"15:04:05",
		),
	)

	fmt.Println(
		"================================",
	)

	fmt.Printf(
		"%-4s %-15s %-9s %-9s %-10s %-10s %-10s\n",
		"#",
		"Symbol",
		"RVOL10",
		"RVOL30",
		"HighRate30",
		"24h%",
		"RankRVOL",
	)

	for i := 0; i < topN; i++ {

		result :=
			results[i]

		priceChange := "--"

		if result.HasPriceChange {
			priceChange =
				fmt.Sprintf(
					"%+.2f%%",
					result.PriceChangePercent,
				)
		}

		fmt.Printf(
			"%-4d %-15s %-9.2f %-9.2f %-10.1f%% %-10s %-10.2f\n",
			i+1,
			result.Symbol,
			result.RVOL10,
			result.RVOL30,
			result.HighRVOLRate30*100,
			priceChange,
			result.RankRVOL,
		)
	}
}

// calculateAllRVOL 计算全部交易对。
func calculateAllRVOL() []RVOLResult {

	statesSnapshot :=
		make(
			[]*SymbolState,
			0,
			len(states),
		)

	for _, state := range states {

		statesSnapshot =
			append(
				statesSnapshot,
				state,
			)
	}

	results :=
		make(
			[]RVOLResult,
			0,
			len(statesSnapshot),
		)

	var mutex sync.Mutex

	var wg sync.WaitGroup

	wg.Add(
		len(statesSnapshot),
	)

	for _, state := range statesSnapshot {

		go func(
			state *SymbolState,
		) {

			defer wg.Done()

			result, ok :=
				calculateSymbolRVOL(
					state,
				)

			if !ok {
				return
			}

			mutex.Lock()

			results =
				append(
					results,
					result,
				)

			mutex.Unlock()

		}(state)
	}

	wg.Wait()

	return results
}

// calculateSymbolRVOL 计算单个交易对。
func calculateSymbolRVOL(
	state *SymbolState,
) (RVOLResult, bool) {

	if state == nil {
		return RVOLResult{}, false
	}

	state.mu.RLock()

	symbol :=
		state.Symbol

	currentVolume :=
		state.CurrentVolume

	records :=
		append(
			[]RVOLRecord(nil),
			state.History.Records...,
		)

	highRVOLRate15 :=
		calculateHighRVOLRate(
			state,
			15,
		)

	highRVOLRate30 :=
		calculateHighRVOLRate(
			state,
			30,
		)

	state.mu.RUnlock()

	if len(records) < 30 {
		return RVOLResult{}, false
	}

	// ========================================
	// 最近 10 根
	// ========================================

	start10 :=
		len(records) - 10

	var sum10 float64

	for i := start10; i < len(records); i++ {

		sum10 +=
			records[i].Volume
	}

	avg10 :=
		sum10 / 10

	// ========================================
	// 最近 30 根
	// ========================================

	start30 :=
		len(records) - 30

	var sum30 float64

	for i := start30; i < len(records); i++ {

		sum30 +=
			records[i].Volume
	}

	avg30 :=
		sum30 / 30

	// ========================================
	// 当前 RVOL
	// ========================================

	rvol10, rvol30, ok :=
		computeRVOL(
			currentVolume,
			avg10,
			avg30,
		)

	if !ok {
		return RVOLResult{}, false
	}

	rankRVOL :=
		rvol10

	if rvol30 > rankRVOL {
		rankRVOL = rvol30
	}

	priceChange,
		hasPriceChange :=
		getPriceChangePercent(
			symbol,
		)

	return RVOLResult{
		Symbol: symbol,

		RVOL10: rvol10,

		RVOL30: rvol30,

		RankRVOL: rankRVOL,

		HighRVOLRate15: highRVOLRate15,

		HighRVOLRate30: highRVOLRate30,

		PriceChangePercent: priceChange,

		HasPriceChange: hasPriceChange,
	}, true
}
