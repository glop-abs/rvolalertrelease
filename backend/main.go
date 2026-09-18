package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var states map[string]*SymbolState

var logFileWriter *dailyFileWriter

func main() {
	cfg, err := loadConfig()

	if err != nil {
		println("配置加载失败:")
		println(err.Error())
		return
	}

	config = cfg

	initLogger(config)

	// ========================================
	// 注册优雅退出
	// ========================================

	sigCh := make(chan os.Signal, 1)

	signal.Notify(
		sigCh,
		os.Interrupt,
		syscall.SIGTERM,
	)

	go func() {
		sig := <-sigCh

		slog.Info("收到退出信号，正在关闭", "signal", sig.String())

		if logFileWriter != nil {
			_ = logFileWriter.Close()
		}

		os.Exit(0)
	}()

	slog.Info("RVOL Alert 启动",
		"version", "0.5.0",
		"api_addr", config.Server.APIAddr,
		"rvol_threshold", config.Alert.RVOLThreshold,
		"ranking_interval", config.RankingInterval().String(),
		"ws_push_interval", config.WSPushInterval().String(),
	)

	// ========================================
	// 第一步：获取交易对
	// ========================================

	symbols, err := getSymbols()

	if err != nil {
		slog.Error("获取交易对失败", "err", err)
		return
	}

	slog.Info("交易对获取完成", "count", len(symbols))

	// ========================================
	// 第二步：建立所有 SymbolState
	// ========================================

	states = make(
		map[string]*SymbolState,
		len(symbols),
	)

	for _, symbol := range symbols {
		states[symbol] = &SymbolState{
			Symbol: symbol,
		}
	}

	slog.Info("SymbolState 创建完成", "count", len(states))

	// ========================================
	// 第三步：初始化全部历史数据
	// ========================================

	err = initializeAllHistory(symbols)

	if err != nil {
		slog.Error("历史数据初始化失败，程序停止", "err", err)
		return
	}

	// ========================================
	// 第四步：启动 HTTP API + WebSocket 服务端
	// ========================================

	go func() {
		if err := startHTTP(config.Server.APIAddr); err != nil {
			slog.Error("HTTP API 停止", "err", err)
		}
	}()

	// ========================================
	// 第五步：启动 Alert 广播
	// ========================================

	go startWSBroadcaster()

	// ========================================
	// 第六步：启动全市场 RVOL 排名
	// ========================================

	go startRVOLRanking()

	// ========================================
	// 第七步：启动 Binance WebSocket
	// ========================================

	err = startWebSocket(symbols)

	if err != nil {
		slog.Error("WebSocket 停止", "err", err)
		return
	}
}
