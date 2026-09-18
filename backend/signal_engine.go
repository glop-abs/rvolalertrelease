package main

// calculateAlertRVOL 计算 Alert 使用的同周期最大 RVOL。
//
// AlertRVOL10 = max(
//
//	上一个 15m 周期最大 RVOL10,
//	当前 15m 周期最大 RVOL10,
//
// )
//
// AlertRVOL30 = max(
//
//	上一个 15m 周期最大 RVOL30,
//	当前 15m 周期最大 RVOL30,
//
// )
//
// 调用者必须持有 state.mu。
func calculateAlertRVOL(
	state *SymbolState,
) (float64, float64) {

	alertRVOL10 := state.PreviousCycleMaxRVOL10

	if state.CurrentCycleMaxRVOL10 > alertRVOL10 {
		alertRVOL10 = state.CurrentCycleMaxRVOL10
	}

	alertRVOL30 := state.PreviousCycleMaxRVOL30

	if state.CurrentCycleMaxRVOL30 > alertRVOL30 {
		alertRVOL30 = state.CurrentCycleMaxRVOL30
	}

	return alertRVOL10, alertRVOL30
}

// calculateHighRVOLRate 计算最近 count 根已完成 Kline 中
// 出现 High RVOL 的比例。
//
// High RVOL 定义：
//
//	RVOL10 >= threshold
//	OR
//	RVOL30 >= threshold
//
// 返回值范围：0 ~ 1。
//
// 例如：
// 最近 30 根里面有 9 根 High RVOL
//
//	9 / 30 = 0.30
//
// 调用者必须持有 state.mu。
func calculateHighRVOLRate(
	state *SymbolState,
	count int,
) float64 {

	if count <= 0 {
		return 0
	}

	records := state.History.Records

	if len(records) < count {
		return 0
	}

	start := len(records) - count

	threshold := config.Alert.RVOLThreshold

	highCount := 0

	for i := start; i < len(records); i++ {
		record := records[i]

		if record.RVOL10 >= threshold ||
			record.RVOL30 >= threshold {
			highCount++
		}
	}

	return float64(highCount) / float64(count)
}

// generateSignal 根据当前周期最大 RVOL 生成 Signal。
//
// Alert 条件：
//
//	RVOL10 >= 阈值
//	OR
//	RVOL30 >= 阈值
//
// 同时计算 HighRVOLRate15 / HighRVOLRate30。
//
// 调用者必须持有 state.mu。
func generateSignal(
	state *SymbolState,
) Signal {

	alertRVOL10, alertRVOL30 :=
		calculateAlertRVOL(state)

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

	threshold := config.Alert.RVOLThreshold

	const epsilon = 1e-9

	triggered :=
		alertRVOL10 >= threshold-epsilon ||
			alertRVOL30 >= threshold-epsilon

	return Signal{
		Symbol: state.Symbol,

		CycleOpenTime: state.CurrentKlineOpenTime,

		AlertRVOL10: alertRVOL10,
		AlertRVOL30: alertRVOL30,

		HighRVOLRate15: highRVOLRate15,
		HighRVOLRate30: highRVOLRate30,

		Triggered: triggered,
	}
}
