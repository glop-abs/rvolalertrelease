package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// upgrader 把 HTTP 升级成 WebSocket。
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024 * 32,

	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// wsClient 是一个 Flutter 客户端连接。
type wsClient struct {
	conn *websocket.Conn

	sendCh chan []byte
}

// wsHub 管理所有 WebSocket 客户端。
type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

var hub = &wsHub{
	clients: make(map[*wsClient]struct{}),
}

// registerWSRoute 注册 /ws。
func registerWSRoute(mux *http.ServeMux) {
	mux.HandleFunc(
		"GET /ws",
		handleWS,
	)
}

// handleWS 处理 Flutter WebSocket。
func handleWS(
	w http.ResponseWriter,
	r *http.Request,
) {

	slog.Info(
		"WS 连接请求",
		"remote",
		r.RemoteAddr,
	)

	conn, err :=
		wsUpgrader.Upgrade(
			w,
			r,
			nil,
		)

	if err != nil {

		slog.Warn(
			"WebSocket 升级失败",
			"err",
			err,
		)

		return
	}

	client := &wsClient{
		conn: conn,

		sendCh: make(
			chan []byte,
			64,
		),
	}

	hub.mu.Lock()

	hub.clients[client] = struct{}{}

	clientCount :=
		len(hub.clients)

	hub.mu.Unlock()

	slog.Info(
		"Flutter 客户端连接",
		"remote",
		r.RemoteAddr,
		"total",
		clientCount,
	)

	// 新客户端立即发送完整 Alert 历史。
	client.sendCh <- buildAlertsSnapshot()

	go client.writeLoop()

	client.readLoop()

	hub.mu.Lock()

	delete(
		hub.clients,
		client,
	)

	clientCount =
		len(hub.clients)

	hub.mu.Unlock()

	close(client.sendCh)

	_ = conn.Close()

	slog.Info(
		"Flutter 客户端断开",
		"remote",
		r.RemoteAddr,
		"total",
		clientCount,
	)
}

// writeLoop 从 sendCh 消费消息。
func (c *wsClient) writeLoop() {

	ticker :=
		time.NewTicker(
			30 * time.Second,
		)

	defer ticker.Stop()

	for {

		select {

		case msg, ok :=
			<-c.sendCh:

			if !ok {
				return
			}

			_ = c.conn.SetWriteDeadline(
				time.Now().Add(
					10 * time.Second,
				),
			)

			if err :=
				c.conn.WriteMessage(
					websocket.TextMessage,
					msg,
				); err != nil {

				return
			}

		case <-ticker.C:

			_ = c.conn.SetWriteDeadline(
				time.Now().Add(
					10 * time.Second,
				),
			)

			if err :=
				c.conn.WriteMessage(
					websocket.PingMessage,
					nil,
				); err != nil {

				return
			}
		}
	}
}

// readLoop 读取客户端消息。
func (c *wsClient) readLoop() {

	c.conn.SetReadLimit(
		1024,
	)

	for {

		_, _, err :=
			c.conn.ReadMessage()

		if err != nil {
			return
		}
	}
}

// broadcastAlerts 广播完整 Alert 快照。
func broadcastAlerts() {

	// 先更新当前 Alert 的实时数据。
	refreshAlertData()

	snapshot :=
		buildAlertsSnapshot()

	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for client := range hub.clients {

		select {

		case client.sendCh <- snapshot:

		default:
			// 队列满则跳过本轮。
		}
	}
}

// buildAlertsSnapshot 构建完整 Alert Batch JSON。
func buildAlertsSnapshot() []byte {

	batches :=
		getAlertBatches()

	items :=
		make(
			[]alertBatchDTO,
			0,
			len(batches),
		)

	for _, batch := range batches {

		alerts :=
			make(
				[]alertDTO,
				0,
				len(batch.Alerts),
			)

		for _, a := range batch.Alerts {

			alerts =
				append(
					alerts,
					alertDTO{
						Symbol: a.Symbol,

						CycleOpenTime: a.CycleOpenTime,

						AlertRVOL10: a.AlertRVOL10,

						AlertRVOL30: a.AlertRVOL30,

						HighRVOLRate15: a.HighRVOLRate15,

						HighRVOLRate30: a.HighRVOLRate30,

						PriceChangePercent: a.PriceChangePercent,

						HasPriceChange: a.HasPriceChange,
					},
				)
		}

		items =
			append(
				items,
				alertBatchDTO{
					CycleOpenTime: batch.CycleOpenTime,

					Count: len(alerts),

					Alerts: alerts,
				},
			)
	}

	resp :=
		alertsHistoryResponse{
			CurrentCycle: currentAlertCycle(),

			BatchCount: len(items),

			Batches: items,
		}

	data, err :=
		json.Marshal(resp)

	if err != nil {

		slog.Error(
			"Alert 快照序列化失败",
			"err",
			err,
		)

		return []byte(
			`{"error":"marshal failed"}`,
		)
	}

	return data
}

// startWSBroadcaster 定期广播完整 Alert。
func startWSBroadcaster() {

	interval :=
		config.WSPushInterval()

	ticker :=
		time.NewTicker(interval)

	defer ticker.Stop()

	slog.Info(
		"Alert WebSocket 广播启动",
		"interval",
		interval,
	)

	for range ticker.C {

		broadcastAlerts()
	}
}
