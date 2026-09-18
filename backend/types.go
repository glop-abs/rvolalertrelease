package main

import "sync"

type Kline struct {
	OpenTime int64

	Open   string
	High   string
	Low    string
	Close  string
	Volume string

	CloseTime int64

	QuoteVolume string

	TradeCount int

	TakerBuyVolume   string
	TakerBuyQuoteVol string
}

type RVOLRecord struct {
	OpenTime int64

	Volume float64

	RVOL10 float64
	RVOL30 float64
}

type RVOLHistory struct {
	Records []RVOLRecord
}

type SymbolState struct {
	// mu 保护当前交易对的全部运行状态。
	mu sync.RWMutex

	Symbol string

	// 已完成 Kline 的 RVOL 历史。
	History RVOLHistory

	// 当前正在形成的 15m Kline。
	CurrentKlineOpenTime int64
	CurrentVolume        float64

	// 当前 15m 周期的最大 RVOL。
	CurrentCycleMaxRVOL10 float64
	CurrentCycleMaxRVOL30 float64

	// 上一个 15m 周期的最大 RVOL。
	PreviousCycleMaxRVOL10 float64
	PreviousCycleMaxRVOL30 float64

	// 当前周期是否已经进入 Alert。
	AlertedThisCycle bool
}

// Signal 表示一次实时 RVOL 判断结果。
type Signal struct {
	Symbol string

	CycleOpenTime int64

	AlertRVOL10 float64
	AlertRVOL30 float64

	// 最近 15 根已完成 Kline 中出现 High RVOL 的比例。
	HighRVOLRate15 float64

	// 最近 30 根已完成 Kline 中出现 High RVOL 的比例。
	HighRVOLRate30 float64

	Triggered bool
}

// AlertRecord 表示 Alert 页面中的一个 ticker。
type AlertRecord struct {
	Symbol string

	CycleOpenTime int64

	AlertRVOL10 float64
	AlertRVOL30 float64

	// 最近 15 根已完成 Kline 的 High RVOL 比率。
	HighRVOLRate15 float64

	// 最近 30 根已完成 Kline 的 High RVOL 比率。
	HighRVOLRate30 float64

	// 24h 涨跌幅（%）。
	PriceChangePercent float64
	HasPriceChange     bool
}

// AlertBatch 表示一个完整的 15m Alert 批次。
type AlertBatch struct {
	CycleOpenTime int64

	Alerts []AlertRecord
}

// RVOLResult 表示一次完整的当前 RVOL 计算结果。
type RVOLResult struct {
	Symbol string

	RVOL10 float64
	RVOL30 float64

	RankRVOL float64

	// 最近 15 / 30 根已完成 Kline 的 High RVOL 比率。
	HighRVOLRate15 float64
	HighRVOLRate30 float64

	// 24h 涨跌幅。
	PriceChangePercent float64
	HasPriceChange     bool
}
