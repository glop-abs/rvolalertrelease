package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// initLogger 根据 config 初始化全局日志。
//
// 输出目标：
//
//  1. 控制台（stdout）
//  2. 文件 ./logs/rvol-YYYY-MM-DD.log（如果可写）
//
// 如果文件不可写，降级为只输出控制台，不报错。
//
// 注意：
//
//	日志文件 writer 会保存到全局 logFileWriter，
//	供 main 在优雅退出时关闭。
func initLogger(cfg Config) {
	var level slog.Level

	switch strings.ToLower(cfg.Log.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	// ========================================
	// 构建输出目标
	// ========================================

	var writers []io.Writer

	// 1. 控制台
	writers = append(writers, os.Stdout)

	// 2. 文件（可选）
	logDir := cfg.Log.Dir
	if logDir == "" {
		logDir = "./logs"
	}

	fileWriter, err := newDailyFileWriter(
		logDir,
		"rvol",
		cfg.Log.RetentionDays,
	)

	if err != nil {
		os.Stderr.WriteString(
			"日志文件不可写，仅输出控制台: " +
				err.Error() + "\n",
		)
	} else {
		writers = append(writers, fileWriter)

		// 保存到全局，供优雅退出关闭。
		logFileWriter = fileWriter

		os.Stdout.WriteString(
			"日志目录: " + logDir + "\n",
		)
	}

	// ========================================
	// 构建 handler
	// ========================================

	multi := io.MultiWriter(writers...)

	var handler slog.Handler

	if strings.ToLower(cfg.Log.Format) == "json" {
		handler = slog.NewJSONHandler(multi, opts)
	} else {
		handler = slog.NewTextHandler(multi, opts)
	}

	slog.SetDefault(slog.New(handler))
}
