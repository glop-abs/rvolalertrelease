package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ============================================================
// HTTP Server
// ============================================================

// startHTTP 启动 HTTP API。
func startHTTP(addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /alerts", handleAlerts)
	mux.HandleFunc("GET /ranking", handleRanking)

	// WebSocket。
	registerWSRoute(mux)

	mux.HandleFunc("OPTIONS /", handlePreflight)

	handler := withCORS(withLogging(mux))

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("HTTP API 启动", "addr", addr)

	return server.ListenAndServe()
}

// ============================================================
// Middleware
// ============================================================

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, OPTIONS",
		)
		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type",
		)
		w.Header().Set(
			"Access-Control-Max-Age",
			"86400",
		)

		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		if r.URL.Path != "/health" &&
			r.URL.Path != "/ws" {

			slog.Info(
				"HTTP",
				"method",
				r.Method,
				"path",
				r.URL.Path,
				"status",
				rw.status,
				"duration_ms",
				time.Since(start).Milliseconds(),
				"remote",
				r.RemoteAddr,
			)
		}
	})
}

// statusRecorder 用于记录 HTTP 状态码。
//
// 同时实现 Hijacker，保证 WebSocket 升级不受影响。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) Hijack() (
	net.Conn,
	*bufio.ReadWriter,
	error,
) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)

	if !ok {
		return nil, nil, fmt.Errorf(
			"底层 ResponseWriter 不支持 Hijacker",
		)
	}

	return hijacker.Hijack()
}

// ============================================================
// Handlers
// ============================================================

func handlePreflight(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// GET /health
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]interface{}{
			"status": "ok",
			"time":   time.Now().Unix(),
			"cycle":  currentAlertCycle(),
		},
	)
}

// ============================================================
// GET /alerts
// ============================================================
//
// 返回 Alert 历史批次。
//
// 每个 15m 周期对应一个 batch。
//
// 批次内部按照 Signal Quality 排序：
//
//  1. HighRVOLRate30 越低越靠前
//  2. HighRVOLRate15 越低越靠前
//  3. 当前最高 RVOL 越高越靠前
//  4. Symbol 升序
//
// 注意：
//
// “异常 RVOL 出现频率越低”代表这个信号越少见，
// 因此在 Alert 页面中优先显示。
func handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]string{
				"error": "method not allowed",
			},
		)
		return
	}

	batches := getAlertBatches()

	items := make(
		[]alertBatchDTO,
		0,
		len(batches),
	)

	for _, batch := range batches {

		alerts := make(
			[]alertDTO,
			0,
			len(batch.Alerts),
		)

		for _, a := range batch.Alerts {

			alerts = append(
				alerts,
				alertDTO{
					Symbol:             a.Symbol,
					CycleOpenTime:      a.CycleOpenTime,
					AlertRVOL10:        a.AlertRVOL10,
					AlertRVOL30:        a.AlertRVOL30,
					HighRVOLRate15:     a.HighRVOLRate15,
					HighRVOLRate30:     a.HighRVOLRate30,
					PriceChangePercent: a.PriceChangePercent,
					HasPriceChange:     a.HasPriceChange,
				},
			)
		}

		// 每个批次内部按照 Signal Quality 排列。
		sort.Slice(
			alerts,
			func(i, j int) bool {

				// 第一优先级：
				// 30 根 K 线中的异常 RVOL 出现率越低越靠前。
				if alerts[i].HighRVOLRate30 !=
					alerts[j].HighRVOLRate30 {

					return alerts[i].HighRVOLRate30 <
						alerts[j].HighRVOLRate30
				}

				// 第二优先级：
				// 15 根 K 线中的异常 RVOL 出现率越低越靠前。
				if alerts[i].HighRVOLRate15 !=
					alerts[j].HighRVOLRate15 {

					return alerts[i].HighRVOLRate15 <
						alerts[j].HighRVOLRate15
				}

				// 第三优先级：
				// 当前最高 RVOL 越高越靠前。
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

				// 最后按照交易对名称稳定排序。
				return alerts[i].Symbol <
					alerts[j].Symbol
			},
		)

		items = append(
			items,
			alertBatchDTO{
				CycleOpenTime: batch.CycleOpenTime,
				Count:         len(alerts),
				Alerts:        alerts,
			},
		)
	}

	// 最新批次放前面。
	sort.Slice(
		items,
		func(i, j int) bool {
			return items[i].CycleOpenTime >
				items[j].CycleOpenTime
		},
	)

	writeJSON(
		w,
		http.StatusOK,
		alertsHistoryResponse{
			CurrentCycle: currentAlertCycle(),
			BatchCount:   len(items),
			Batches:      items,
		},
	)
}

// ============================================================
// GET /ranking
// ============================================================
//
// 返回当前所有交易对的 RVOL 排名。
//
// Ranking 与 Alert 的用途不同：
//
// Alert：
//
//	按 Signal Quality 排序
//	异常频率越低越靠前
//
// Ranking：
//
//	按当前 RVOL 排序
//	RVOL 越高越靠前
//
// 支持：
//
//	/ranking
//	/ranking?limit=10
//	/ranking?limit=50
func handleRanking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			map[string]string{
				"error": "method not allowed",
			},
		)
		return
	}

	results := calculateAllRVOL()

	// RankRVOL 是排名使用的 RVOL。
	//
	// 当前定义：
	//
	// RankRVOL = max(RVOL10, RVOL30)
	//
	// RVOL 越高，排名越靠前。
	sort.Slice(
		results,
		func(i, j int) bool {

			if results[i].RankRVOL !=
				results[j].RankRVOL {

				return results[i].RankRVOL >
					results[j].RankRVOL
			}

			return results[i].Symbol <
				results[j].Symbol
		},
	)

	limit := len(results)

	if value := r.URL.Query().Get("limit"); value != "" {

		n, err := strconv.Atoi(value)

		if err == nil && n > 0 && n < limit {
			limit = n
		}
	}

	items := make(
		[]rankingDTO,
		0,
		limit,
	)

	for i := 0; i < limit; i++ {

		result := results[i]

		// 24h 涨跌幅来自 ticker cache。
		//
		// ticker WS 是异步更新的，所以刚启动时
		// 可能暂时没有数据。
		priceChange, hasPriceChange :=
			getPriceChangePercent(result.Symbol)

		items = append(
			items,
			rankingDTO{
				Rank:               i + 1,
				Symbol:             result.Symbol,
				RVOL10:             result.RVOL10,
				RVOL30:             result.RVOL30,
				RankRVOL:           result.RankRVOL,
				HighRVOLRate15:     result.HighRVOLRate15,
				HighRVOLRate30:     result.HighRVOLRate30,
				PriceChangePercent: priceChange,
				HasPriceChange:     hasPriceChange,
			},
		)
	}

	writeJSON(
		w,
		http.StatusOK,
		rankingResponse{
			Count:   len(items),
			Ranking: items,
		},
	)
}

// ============================================================
// Alert DTO
// ============================================================

// 单个 Alert。
type alertDTO struct {
	Symbol string `json:"symbol"`

	CycleOpenTime int64 `json:"cycle_open_time"`

	AlertRVOL10 float64 `json:"alert_rvol10"`

	AlertRVOL30 float64 `json:"alert_rvol30"`

	HighRVOLRate15 float64 `json:"high_rvol_rate15"`

	HighRVOLRate30 float64 `json:"high_rvol_rate30"`

	PriceChangePercent float64 `json:"price_change_percent"`

	HasPriceChange bool `json:"has_price_change"`
}

// 一个 15m 周期的 Alert 批次。
type alertBatchDTO struct {
	CycleOpenTime int64 `json:"cycle_open_time"`

	Count int `json:"count"`

	Alerts []alertDTO `json:"alerts"`
}

// Alert 历史批次响应。
type alertsHistoryResponse struct {
	CurrentCycle int64 `json:"current_cycle"`

	BatchCount int `json:"batch_count"`

	Batches []alertBatchDTO `json:"batches"`
}

// ============================================================
// Ranking DTO
// ============================================================

type rankingDTO struct {
	Rank int `json:"rank"`

	Symbol string `json:"symbol"`

	RVOL10 float64 `json:"rvol10"`

	RVOL30 float64 `json:"rvol30"`

	RankRVOL float64 `json:"rank_rvol"`

	// 最近 15 根完成 K 线中，
	// RVOL 达到异常阈值的比例。
	HighRVOLRate15 float64 `json:"high_rvol_rate15"`

	// 最近 30 根完成 K 线中，
	// RVOL 达到异常阈值的比例。
	HighRVOLRate30 float64 `json:"high_rvol_rate30"`

	// Binance 24h ticker 涨跌幅。
	PriceChangePercent float64 `json:"price_change_percent"`

	// 是否已经获得有效的 24h ticker 数据。
	HasPriceChange bool `json:"has_price_change"`
}

type rankingResponse struct {
	Count int `json:"count"`

	Ranking []rankingDTO `json:"ranking"`
}

// ============================================================
// JSON
// ============================================================

func writeJSON(
	w http.ResponseWriter,
	status int,
	data interface{},
) {
	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	w.WriteHeader(status)

	encoder := json.NewEncoder(w)

	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(data); err != nil {
		slog.Error(
			"JSON 编码失败",
			"err",
			err,
		)
	}
}
