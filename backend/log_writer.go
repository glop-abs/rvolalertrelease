package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dailyFileWriter 是按天轮转的日志 writer。
//
// 同一天写同一个文件：rvol-2026-09-16.log
// 跨天时自动切换到新文件。
//
// 保留天数来自 config.Log.RetentionDays。
//
// 所有方法都是并发安全的。
type dailyFileWriter struct {
	mu sync.Mutex

	dir         string
	prefix      string
	retention   int
	currentDate string
	file        *os.File
}

// newDailyFileWriter 创建按天轮转的 writer。
//
// 如果目录不可写，返回 error，调用方可以降级到"只输出控制台"。
func newDailyFileWriter(
	dir string,
	prefix string,
	retentionDays int,
) (*dailyFileWriter, error) {

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf(
			"创建日志目录失败 %s: %w",
			dir,
			err,
		)
	}

	w := &dailyFileWriter{
		dir:       dir,
		prefix:    prefix,
		retention: retentionDays,
	}

	// 启动时立刻清理过期日志。
	w.cleanupOldLogs()

	// 打开今天的日志文件。
	if err := w.rotateIfNeeded(); err != nil {
		return nil, err
	}

	return w, nil
}

// Write 实现 io.Writer。
func (w *dailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否需要跨天轮转。
	if err := w.rotateIfNeeded(); err != nil {
		// 轮转失败也不能阻塞日志，
		// 写到 stderr 让用户知道。
		fmt.Fprintf(os.Stderr, "日志轮转失败: %v\n", err)
	}

	if w.file == nil {
		return len(p), nil
	}

	return w.file.Write(p)
}

// Close 关闭当前文件。
func (w *dailyFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}

	return nil
}

// rotateIfNeeded 检查日期，必要时切换文件。
//
// 调用者必须持有 w.mu（除了 newDailyFileWriter 里的首次调用）。
func (w *dailyFileWriter) rotateIfNeeded() error {
	today := time.Now().Format("2006-01-02")

	if w.currentDate == today && w.file != nil {
		return nil
	}

	// 关闭旧文件。
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	path := filepath.Join(
		w.dir,
		fmt.Sprintf("%s-%s.log", w.prefix, today),
	)

	f, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)

	if err != nil {
		return fmt.Errorf(
			"打开日志文件失败 %s: %w",
			path,
			err,
		)
	}

	w.file = f
	w.currentDate = today

	// 跨天时顺便清理一次。
	w.cleanupOldLogsLocked()

	return nil
}

// cleanupOldLogs 清理过期日志（加锁版）。
func (w *dailyFileWriter) cleanupOldLogs() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.cleanupOldLogsLocked()
}

// cleanupOldLogsLocked 清理过期日志。
//
// 调用者必须持有 w.mu。
func (w *dailyFileWriter) cleanupOldLogsLocked() {
	if w.retention <= 0 {
		return
	}

	cutoff := time.Now().
		AddDate(0, 0, -w.retention).
		Format("2006-01-02")

	entries, err := os.ReadDir(w.dir)

	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// 只处理 prefix-YYYY-MM-DD.log 格式。
		if !strings.HasPrefix(name, w.prefix+"-") {
			continue
		}

		if !strings.HasSuffix(name, ".log") {
			continue
		}

		// 提取日期部分。
		datePart := strings.TrimSuffix(
			strings.TrimPrefix(name, w.prefix+"-"),
			".log",
		)

		if len(datePart) != 10 {
			continue
		}

		// 文件名日期 < cutoff，删除。
		if datePart < cutoff {
			path := filepath.Join(w.dir, name)
			_ = os.Remove(path)
		}
	}
}
