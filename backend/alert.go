package main

import (
	"sort"
	"sync"
)

// AlertHistoryLimit 控制内存中保存多少个 15m Alert 批次。
// 20 个批次 = 最近约 5 小时。
const AlertHistoryLimit = 20

// AlertManager 管理 Alert Batch 历史。
type AlertManager struct {
	mu sync.RWMutex

	// 当前正在形成的 15m 周期。
	CurrentCycleOpenTime int64

	// 最近的 Alert Batch。
	// 第 0 个为最新批次。
	Batches []AlertBatch
}

var alertManager = AlertManager{
	Batches: make([]AlertBatch, 0, AlertHistoryLimit),
}

// updateAlert 根据实时 Signal 更新 Alert。
//
// 返回：
//
//	true  = 这个 ticker 第一次进入当前 Batch
//	false = ticker 已经存在，只是更新数据
func updateAlert(signal Signal) bool {

	if !signal.Triggered {
		return false
	}

	alertManager.mu.Lock()
	defer alertManager.mu.Unlock()

	// ========================================
	// 初始化
	// ========================================

	if alertManager.CurrentCycleOpenTime == 0 {
		alertManager.CurrentCycleOpenTime =
			signal.CycleOpenTime
	}

	// ========================================
	// 进入新的 15m 周期
	// ========================================

	if signal.CycleOpenTime !=
		alertManager.CurrentCycleOpenTime {

		alertManager.CurrentCycleOpenTime =
			signal.CycleOpenTime

		newBatch := AlertBatch{
			CycleOpenTime: signal.CycleOpenTime,
			Alerts:        make([]AlertRecord, 0),
		}

		// 最新 Batch 放在最前面。
		alertManager.Batches = append(
			[]AlertBatch{newBatch},
			alertManager.Batches...,
		)

		// 限制历史数量。
		if len(alertManager.Batches) >
			AlertHistoryLimit {

			alertManager.Batches =
				alertManager.Batches[:AlertHistoryLimit]
		}
	}

	// ========================================
	// 确保当前 Batch 存在
	// ========================================

	if len(alertManager.Batches) == 0 ||
		alertManager.Batches[0].CycleOpenTime !=
			signal.CycleOpenTime {

		batch := AlertBatch{
			CycleOpenTime: signal.CycleOpenTime,
			Alerts:        make([]AlertRecord, 0),
		}

		alertManager.Batches = append(
			[]AlertBatch{batch},
			alertManager.Batches...,
		)
	}

	// ========================================
	// 获取 24h 涨跌幅
	// ========================================

	priceChange, hasPriceChange :=
		getPriceChangePercent(signal.Symbol)

	record := AlertRecord{
		Symbol: signal.Symbol,

		CycleOpenTime: signal.CycleOpenTime,

		AlertRVOL10: signal.AlertRVOL10,

		AlertRVOL30: signal.AlertRVOL30,

		HighRVOLRate15: signal.HighRVOLRate15,

		HighRVOLRate30: signal.HighRVOLRate30,

		PriceChangePercent: priceChange,

		HasPriceChange: hasPriceChange,
	}

	currentBatch := &alertManager.Batches[0]

	// ========================================
	// 查找 ticker
	// ========================================

	for i := range currentBatch.Alerts {

		if currentBatch.Alerts[i].Symbol !=
			signal.Symbol {
			continue
		}

		// 已存在：持续更新实时数据。
		currentBatch.Alerts[i] = record

		sortAlertRecords(currentBatch.Alerts)

		return false
	}

	// ========================================
	// 新 ticker
	// ========================================

	currentBatch.Alerts =
		append(
			currentBatch.Alerts,
			record,
		)

	sortAlertRecords(currentBatch.Alerts)

	return true
}

// sortAlertRecords 按 Signal Quality 排序。
//
// 排序原则：
//
//  1. HighRVOLRate30 越低越靠前。
//     异常 RVOL 出现得越少，这次 Signal 越稀有。
//  2. HighRVOLRate15 越低越靠前。
//  3. 当前最高 RVOL 越高越靠前。
//  4. Symbol 字母序。
func sortAlertRecords(
	alerts []AlertRecord,
) {

	sort.Slice(
		alerts,
		func(i, j int) bool {

			if alerts[i].HighRVOLRate30 !=
				alerts[j].HighRVOLRate30 {

				return alerts[i].HighRVOLRate30 <
					alerts[j].HighRVOLRate30
			}

			if alerts[i].HighRVOLRate15 !=
				alerts[j].HighRVOLRate15 {

				return alerts[i].HighRVOLRate15 <
					alerts[j].HighRVOLRate15
			}

			rvolI := alerts[i].AlertRVOL10

			if alerts[i].AlertRVOL30 > rvolI {
				rvolI = alerts[i].AlertRVOL30
			}

			rvolJ := alerts[j].AlertRVOL10

			if alerts[j].AlertRVOL30 > rvolJ {
				rvolJ = alerts[j].AlertRVOL30
			}

			if rvolI != rvolJ {
				return rvolI > rvolJ
			}

			return alerts[i].Symbol <
				alerts[j].Symbol
		},
	)
}

// refreshAlertData 刷新当前所有 Alert 的实时数据。
//
// 这个函数非常重要：
// Alert 第一次触发之后，RVOL 继续变化时，
// 也会同步更新到 Alert 页面。
func refreshAlertData() {

	alertManager.mu.Lock()
	defer alertManager.mu.Unlock()

	if len(alertManager.Batches) == 0 {
		return
	}

	currentBatch :=
		&alertManager.Batches[0]

	for i := range currentBatch.Alerts {

		alert := &currentBatch.Alerts[i]

		state, ok := states[alert.Symbol]

		if !ok {
			continue
		}

		state.mu.RLock()

		signal := generateSignal(state)

		state.mu.RUnlock()

		// 即使当前瞬间低于阈值，
		// 也不要把已经产生过的 Alert 删除。
		//
		// Alert 是事件记录，而不是实时过滤器。
		alert.AlertRVOL10 =
			signal.AlertRVOL10

		alert.AlertRVOL30 =
			signal.AlertRVOL30

		alert.HighRVOLRate15 =
			signal.HighRVOLRate15

		alert.HighRVOLRate30 =
			signal.HighRVOLRate30

		priceChange, hasPriceChange :=
			getPriceChangePercent(
				alert.Symbol,
			)

		alert.PriceChangePercent =
			priceChange

		alert.HasPriceChange =
			hasPriceChange
	}

	sortAlertRecords(
		currentBatch.Alerts,
	)
}

// refreshAlertPriceChange 刷新 Alert 的 24h 涨跌幅。
//
// 保留这个函数用于 ticker 更新时快速刷新。
func refreshAlertPriceChange() {

	alertManager.mu.Lock()
	defer alertManager.mu.Unlock()

	for batchIndex := range alertManager.Batches {

		for i := range alertManager.Batches[batchIndex].Alerts {

			alert :=
				&alertManager.Batches[batchIndex].Alerts[i]

			priceChange, hasPriceChange :=
				getPriceChangePercent(
					alert.Symbol,
				)

			alert.PriceChangePercent =
				priceChange

			alert.HasPriceChange =
				hasPriceChange
		}
	}
}

// getAlertBatches 返回完整 Alert Batch 快照。
func getAlertBatches() []AlertBatch {

	alertManager.mu.RLock()
	defer alertManager.mu.RUnlock()

	result := make(
		[]AlertBatch,
		len(alertManager.Batches),
	)

	for i, batch := range alertManager.Batches {

		result[i] = AlertBatch{
			CycleOpenTime: batch.CycleOpenTime,

			Alerts: append(
				[]AlertRecord(nil),
				batch.Alerts...,
			),
		}
	}

	return result
}

// getCurrentAlerts 返回当前 15m Batch 的 Alert。
func getCurrentAlerts() []AlertRecord {

	alertManager.mu.RLock()
	defer alertManager.mu.RUnlock()

	if len(alertManager.Batches) == 0 {
		return nil
	}

	alerts := append(
		[]AlertRecord(nil),
		alertManager.Batches[0].Alerts...,
	)

	return alerts
}

// currentAlertCycle 返回当前 Alert 周期。
func currentAlertCycle() int64 {

	alertManager.mu.RLock()
	defer alertManager.mu.RUnlock()

	return alertManager.CurrentCycleOpenTime
}
