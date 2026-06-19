// Package config 加载并管理网关配置。
//
// 配置来源优先级（高→低）：环境变量 > .env 文件 > 代码默认值。
// 运行时可通过管理 API 动态覆盖部分配置（如默认 RPM、同步周期）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config 网关全局配置。
type Config struct {
	// Server HTTP 服务配置
	Server ServerConfig

	// Upstream NVIDIA 上游配置
	Upstream UpstreamConfig

	// Scheduler 调度层配置
	Scheduler SchedulerConfig

	// Data 数据层配置
	Data DataConfig

	// Auth 鉴权配置
	Auth AuthConfig
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	// Port 监听端口
	Port int
	// AdminToken 管理 API 鉴权令牌（增删 Key 等敏感操作）
	AdminToken string
}

// UpstreamConfig NVIDIA 上游配置。
type UpstreamConfig struct {
	// ChatBaseURL 对话/补全/模型列表上游
	ChatBaseURL string
	// RetrievalBaseURL 嵌入/重排序上游（不同域名）
	RetrievalBaseURL string
	// DefaultRPM 每 Key 默认每分钟请求数上限（NVIDIA 免费层 40）
	DefaultRPM int
	// RequestTimeout 单次上游请求超时
	RequestTimeout time.Duration
	// ModelSyncInterval 模型目录同步间隔（默认 24h）
	ModelSyncInterval time.Duration
}

// SchedulerConfig 调度层配置。
type SchedulerConfig struct {
	// DefaultConcurrency 默认 N 路并发数（先到先得）
	DefaultConcurrency int
	// MaxConcurrency 最大并发上限（防失控）
	MaxConcurrency int
	// RequestTimeout 整体请求超时（含重试）
	RequestTimeout time.Duration
	// CircuitThreshold 连续失败多少次触发熔断
	CircuitThreshold int
	// CircuitCooldown 初始冷却时长（指数退避起始）
	CircuitCooldown time.Duration
}

// DataConfig 数据层配置。
type DataConfig struct {
	// SQLitePath SQLite 数据库文件路径
	SQLitePath string
}

// AuthConfig 鉴权配置。
type AuthConfig struct {
	// DownstreamKeyPrefix 签发给客户端的下游凭证前缀
	DownstreamKeyPrefix string
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (*Config, error) {
	// .env 可选（容器/裸机可能只用环境变量）
	_ = godotenv.Load()

	cfg := &Config{
		Server: ServerConfig{
			Port:       envInt("PORT", 8787),
			// 默认初始令牌 "admin"：首次登录后强制修改并写入 bcrypt 哈希。
			AdminToken: envStr("ADMIN_TOKEN", "admin"),
		},
		Upstream: UpstreamConfig{
			ChatBaseURL:        envStr("NVIDIA_CHAT_BASE_URL", "https://integrate.api.nvidia.com/v1"),
			RetrievalBaseURL:   envStr("NVIDIA_RETRIEVAL_BASE_URL", "https://ai.api.nvidia.com/v1"),
			DefaultRPM:         envInt("DEFAULT_RPM", 40),
			RequestTimeout:     envDuration("UPSTREAM_TIMEOUT", 120*time.Second),
			ModelSyncInterval:  envDuration("MODEL_SYNC_INTERVAL", 24*time.Hour),
		},
		Scheduler: SchedulerConfig{
			DefaultConcurrency: envInt("DEFAULT_CONCURRENCY", 5),
			MaxConcurrency:     envInt("MAX_CONCURRENCY", 10),
			RequestTimeout:     envDuration("SCHEDULER_TIMEOUT", 180*time.Second),
			CircuitThreshold:   envInt("CIRCUIT_THRESHOLD", 5),
			CircuitCooldown:    envDuration("CIRCUIT_COOLDOWN", 30*time.Second),
		},
		Data: DataConfig{
			SQLitePath: envStr("SQLITE_PATH", "data/nslmcrs.db"),
		},
		Auth: AuthConfig{
			DownstreamKeyPrefix: envStr("DOWNSTREAM_KEY_PREFIX", "sk-nv-"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 校验配置完整性。
func (c *Config) validate() error {
	var errs []string
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		errs = append(errs, "PORT 必须在 1-65535 之间")
	}
	if c.Upstream.DefaultRPM <= 0 {
		errs = append(errs, "DEFAULT_RPM 必须 > 0")
	}
	if c.Upstream.ChatBaseURL == "" {
		errs = append(errs, "NVIDIA_CHAT_BASE_URL 不能为空")
	}
	if c.Scheduler.DefaultConcurrency <= 0 {
		errs = append(errs, "DEFAULT_CONCURRENCY 必须 > 0")
	}
	if c.Scheduler.MaxConcurrency < c.Scheduler.DefaultConcurrency {
		errs = append(errs, "MAX_CONCURRENCY 必须 >= DEFAULT_CONCURRENCY")
	}
	if len(errs) > 0 {
		return fmt.Errorf("配置校验失败: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- 环境变量辅助函数 ---

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
