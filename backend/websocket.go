package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	binanceWebSocketURL = "wss://fstream.binance.com/market/ws"

	writeTimeout = 10 * time.Second
)

// BinanceKlineMessage 是 Binance WebSocket Kline 消息。
type BinanceKlineMessage struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`

	Kline BinanceKline `json:"k"`
}

// BinanceKline 是 Binance 15m Kline 数据。
type BinanceKline struct {
	StartTime int64 `json:"t"`
	CloseTime int64 `json:"T"`

	Symbol string `json:"s"`

	Interval string `json:"i"`

	FirstTradeID int64 `json:"f"`
	LastTradeID  int64 `json:"L"`

	Open  string `json:"o"`
	Close string `json:"c"`

	High string `json:"h"`
	Low  string `json:"l"`

	Volume string `json:"v"`

	TradeCount int `json:"n"`

	IsClosed bool `json:"x"`

	QuoteVolume string `json:"q"`

	TakerBuyVolume   string `json:"V"`
	TakerBuyQuoteVol string `json:"Q"`

	Ignore string `json:"B"`
}

type SubscribeRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     int      `json:"id"`
}

// startWebSocket 启动 Binance 全市场 15m Kline +
// 全市场 ticker WebSocket。
func startWebSocket(symbols []string) error {

	u, err := url.Parse(binanceWebSocketURL)

	if err != nil {
		return fmt.Errorf(
			"解析 WebSocket URL 失败: %w",
			err,
		)
	}

	slog.Info(
		"连接 Binance WebSocket",
		"url",
		u.String(),
	)

	conn, _, err :=
		websocket.DefaultDialer.Dial(
			u.String(),
			nil,
		)

	if err != nil {
		return fmt.Errorf(
			"WebSocket 连接失败: %w",
			err,
		)
	}

	defer conn.Close()

	slog.Info("WebSocket 连接成功")

	// ========================================
	// 创建全部订阅
	// ========================================

	streams := make(
		[]string,
		0,
		len(symbols)+1,
	)

	for _, symbol := range symbols {
		stream :=
			strings.ToLower(symbol) +
				"@kline_15m"

		streams =
			append(streams, stream)
	}

	// 全市场 24h ticker。
	streams =
		append(
			streams,
			"!ticker@arr",
		)

	slog.Info(
		"准备订阅 stream",
		"count",
		len(streams),
	)

	// ========================================
	// 分批 SUBSCRIBE
	// ========================================

	batchSize :=
		config.WebSocket.SubscribeBatchSize

	if batchSize <= 0 {
		batchSize = 100
	}

	subscribeID := 1

	for start := 0; start < len(streams); start += batchSize {

		end := start + batchSize

		if end > len(streams) {
			end = len(streams)
		}

		batch := streams[start:end]

		request := SubscribeRequest{
			Method: "SUBSCRIBE",
			Params: batch,
			ID:     subscribeID,
		}

		data, err := json.Marshal(request)

		if err != nil {
			return fmt.Errorf(
				"编码订阅请求失败: %w",
				err,
			)
		}

		if err :=
			conn.WriteMessage(
				websocket.TextMessage,
				data,
			); err != nil {

			return fmt.Errorf(
				"发送订阅请求失败: %w",
				err,
			)
		}

		slog.Info(
			"已发订阅请求",
			"range",
			fmt.Sprintf(
				"%d~%d",
				start+1,
				end,
			),
			"total",
			len(streams),
		)

		subscribeID++

		time.Sleep(
			200 * time.Millisecond,
		)
	}

	// ========================================
	// 查询当前订阅
	// ========================================

	listRequest := SubscribeRequest{
		Method: "LIST_SUBSCRIPTIONS",
		Params: []string{},
		ID:     subscribeID,
	}

	listData, err :=
		json.Marshal(listRequest)

	if err != nil {
		return fmt.Errorf(
			"编码 LIST_SUBSCRIPTIONS 请求失败: %w",
			err,
		)
	}

	if err :=
		conn.WriteMessage(
			websocket.TextMessage,
			listData,
		); err != nil {

		return fmt.Errorf(
			"发送 LIST_SUBSCRIPTIONS 请求失败: %w",
			err,
		)
	}

	// ========================================
	// ping
	// ========================================

	done := make(chan struct{})
	defer close(done)

	go func() {

		ticker :=
			time.NewTicker(
				config.PingInterval(),
			)

		defer ticker.Stop()

		for {
			select {

			case <-done:
				return

			case <-ticker.C:

				_ = conn.SetWriteDeadline(
					time.Now().Add(writeTimeout),
				)

				if err :=
					conn.WriteMessage(
						websocket.PingMessage,
						nil,
					); err != nil {

					slog.Warn(
						"ping 失败",
						"err",
						err,
					)

					return
				}
			}
		}
	}()

	// ========================================
	// 开始接收数据
	// ========================================

	slog.Info(
		"开始接收 WebSocket 数据",
	)

	for {

		if err :=
			conn.SetReadDeadline(
				time.Now().Add(
					config.ReadTimeout(),
				),
			); err != nil {

			return fmt.Errorf(
				"设置读取超时失败: %w",
				err,
			)
		}

		_, message, err :=
			conn.ReadMessage()

		if err != nil {
			return fmt.Errorf(
				"WebSocket 读取失败: %w",
				err,
			)
		}

		handleWebSocketMessage(message)
	}
}

// handleWebSocketMessage 分派不同消息类型。
func handleWebSocketMessage(
	message []byte,
) {

	i := 0

	for i < len(message) &&
		(message[i] == ' ' ||
			message[i] == '\n' ||
			message[i] == '\r' ||
			message[i] == '\t') {

		i++
	}

	if i >= len(message) {
		return
	}

	switch message[i] {

	case '[':
		handleTickerArray(message)

	case '{':
		handleKlineMessage(message)

	default:
	}
}

// handleTickerArray 处理 !ticker@arr。
func handleTickerArray(
	message []byte,
) {

	var raw []map[string]interface{}

	if err := json.Unmarshal(
		message,
		&raw,
	); err != nil {
		return
	}

	tickers := parseTickerArray(raw)

	updateTickerCache(tickers)

	// 刷新所有 Alert 的 24h 涨跌幅。
	refreshAlertPriceChange()
}

// handleKlineMessage 处理 Kline。
func handleKlineMessage(
	message []byte,
) {

	var event BinanceKlineMessage

	if err := json.Unmarshal(
		message,
		&event,
	); err != nil {
		return
	}

	if event.EventType != "kline" {
		return
	}

	handleWebSocketKline(
		event.Symbol,
		event.Kline,
	)
}

// handleWebSocketKline 处理单个交易对的实时 Kline。
func handleWebSocketKline(
	symbol string,
	binanceKline BinanceKline,
) {

	state, ok := states[symbol]

	if !ok {
		return
	}

	currentVolume :=
		volumeToFloat(
			binanceKline.Volume,
		)

	state.mu.Lock()

	// ========================================
	// 判断是否进入新 15m 周期
	// ========================================

	newCycle :=
		state.CurrentKlineOpenTime !=
			binanceKline.StartTime

	if newCycle {

		state.PreviousCycleMaxRVOL10 =
			state.CurrentCycleMaxRVOL10

		state.PreviousCycleMaxRVOL30 =
			state.CurrentCycleMaxRVOL30

		state.CurrentCycleMaxRVOL10 = 0
		state.CurrentCycleMaxRVOL30 = 0

		state.CurrentKlineOpenTime =
			binanceKline.StartTime

		state.AlertedThisCycle = false
	}

	state.CurrentVolume =
		currentVolume

	rvol10, rvol30, err :=
		calculateNextRVOL(
			&state.History,
			currentVolume,
		)

	if err != nil {
		state.mu.Unlock()
		return
	}

	if rvol10 >
		state.CurrentCycleMaxRVOL10 {

		state.CurrentCycleMaxRVOL10 =
			rvol10
	}

	if rvol30 >
		state.CurrentCycleMaxRVOL30 {

		state.CurrentCycleMaxRVOL30 =
			rvol30
	}

	signal :=
		generateSignal(state)

	// ========================================
	// Alert
	// ========================================
	//
	// 注意：
	// 这里不能再使用：
	//
	//     if signal.Triggered &&
	//         !state.AlertedThisCycle
	//
	// 因为 Alert 进入之后，
	// 后续 RVOL 继续增长也必须更新。
	//
	// 所以：
	//
	//     第一次触发 -> 新 Alert
	//     后续触发 -> 更新已有 Alert
	//

	isNewAlert := false

	if signal.Triggered {

		isNewAlert =
			updateAlert(signal)

		state.AlertedThisCycle = true
	}

	state.mu.Unlock()

	// ========================================
	// 新 Alert
	// ========================================

	if isNewAlert {

		fmt.Printf(
			"ALERT [%s] | RVOL10=%.2f RVOL30=%.2f | HighRVOL30=%.1f%% | Cycle=%d\n",
			signal.Symbol,
			signal.AlertRVOL10,
			signal.AlertRVOL30,
			signal.HighRVOLRate30*100,
			signal.CycleOpenTime,
		)

		broadcastAlerts()
	}

	// ========================================
	// 已经存在的 Alert 也需要实时推送
	// ========================================
	//
	// 当前 Kline 每次更新都会重新计算 RVOL。
	// 如果该 ticker 已经进入 Alert，
	// refreshAlertData 会更新它。
	//
	// 这里不需要每次都单独广播，
	// 由固定的 WS broadcaster 每 5 秒推送。
	//

	// ========================================
	// Kline 收盘后写入历史
	// ========================================

	if !binanceKline.IsClosed {
		return
	}

	record := RVOLRecord{
		OpenTime: binanceKline.StartTime,
		Volume:   currentVolume,
		RVOL10:   rvol10,
		RVOL30:   rvol30,
	}

	state.mu.Lock()

	state.History.AddOrUpdate(
		record,
	)

	state.mu.Unlock()
}
