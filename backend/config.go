package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Alert     AlertConfig     `yaml:"alert"`
	Ranking   RankingConfig   `yaml:"ranking"`
	WebSocket WebSocketConfig `yaml:"websocket"`
	Log       LogConfig       `yaml:"log"`
}

type ServerConfig struct {
	APIAddr string `yaml:"api_addr"`
}

type AlertConfig struct {
	RVOLThreshold float64 `yaml:"rvol_threshold"`
}

type RankingConfig struct {
	IntervalSeconds int `yaml:"interval_seconds"`
}

type WebSocketConfig struct {
	SubscribeBatchSize int `yaml:"subscribe_batch_size"`
	ReadTimeoutSeconds int `yaml:"read_timeout_seconds"`
	PingIntervalSecs   int `yaml:"ping_interval_seconds"`

	// 向 Flutter 推送 Alert 的间隔（秒）。
	PushIntervalSeconds int `yaml:"push_interval_seconds"`
}

type LogConfig struct {
	Level         string `yaml:"level"`
	Format        string `yaml:"format"`
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			APIAddr: ":8080",
		},
		Alert: AlertConfig{
			RVOLThreshold: 2.0,
		},
		Ranking: RankingConfig{
			IntervalSeconds: 60,
		},
		WebSocket: WebSocketConfig{
			SubscribeBatchSize:  100,
			ReadTimeoutSeconds:  180,
			PingIntervalSecs:    30,
			PushIntervalSeconds: 5,
		},
		Log: LogConfig{
			Level:         "info",
			Format:        "text",
			Dir:           "./logs",
			RetentionDays: 1,
		},
	}
}

var config = defaultConfig()

func loadConfig() (Config, error) {
	cfg := defaultConfig()

	path, explicit := findConfigPath()

	if path == "" {
		fmt.Println("未找到 config.yaml，使用默认配置")
		return cfg, nil
	}

	data, err := os.ReadFile(path)

	if err != nil {
		if explicit {
			return cfg, fmt.Errorf(
				"读取配置文件失败 %s: %w",
				path,
				err,
			)
		}

		fmt.Printf(
			"读取配置文件失败 %s: %v，使用默认配置\n",
			path,
			err,
		)
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf(
			"解析配置文件失败 %s: %w",
			path,
			err,
		)
	}

	fmt.Printf("已加载配置文件: %s\n", path)

	return cfg, nil
}

func findConfigPath() (string, bool) {
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1], true
		}

		const prefix = "--config="

		if len(arg) > len(prefix) && arg[:len(prefix)] == prefix {
			return arg[len(prefix):], true
		}
	}

	if p := os.Getenv("RVOLALERT_CONFIG"); p != "" {
		return p, true
	}

	if p := "./config.yaml"; fileExists(p) {
		return p, false
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		p := filepath.Join(exeDir, "config.yaml")

		if fileExists(p) {
			return p, false
		}
	}

	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	if err != nil {
		return false
	}

	return !info.IsDir()
}

// ============================================================
// Config 便捷方法
// ============================================================

func (c Config) RankingInterval() time.Duration {
	return time.Duration(c.Ranking.IntervalSeconds) * time.Second
}

func (c Config) ReadTimeout() time.Duration {
	return time.Duration(c.WebSocket.ReadTimeoutSeconds) * time.Second
}

func (c Config) PingInterval() time.Duration {
	return time.Duration(c.WebSocket.PingIntervalSecs) * time.Second
}

func (c Config) WSPushInterval() time.Duration {
	return time.Duration(c.WebSocket.PushIntervalSeconds) * time.Second
}
