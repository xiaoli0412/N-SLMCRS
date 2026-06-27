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

	// AutoPilot 智能调度（Auto-Pilot）配置
	AutoPilot AutoPilotConfig

	// Backup 数据库备份配置（v0.8）
	Backup BackupConfig

	// ModelHealth 模型级健康扫描与熔断配置（v0.9）
	ModelHealth ModelHealthConfig

	// Logging 日志配置（v0.9）
	Logging LoggingConfig

	// Hooks 集成钩子配置（v0.10）
	Hooks HooksConfig
}

// HooksConfig 集成钩子配置（v0.10 新增）。
type HooksConfig struct {
	// WebhookURL 事件回调地址；空则禁用 webhook
	WebhookURL string
	// WebhookSecret HMAC-SHA256 签名密钥
	WebhookSecret string
	// WebhookEvents 触发事件（逗号分隔：success,error,rate_limited,circuit）；空=全部
	WebhookEvents string
}

// LoggingConfig 日志配置（v0.9）。
type LoggingConfig struct {
	// Level 日志级别：debug / info / warn / error（默认 info）
	Level string
	// Format 日志格式：json / text（默认 json）
	Format string
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	// Port 监听端口
	Port int
	// AdminToken 管理 API 鉴权令牌（增删 Key 等敏感操作）
	AdminToken string
}

// DefaultAdminToken 首次启动的初始管理令牌。
// 未设置 ADMIN_TOKEN 时使用；首次登录后必须强制改密，改密前所有受保护管理 API 被锁定。
const DefaultAdminToken = "ADMIN"

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

// AutoPilotConfig 智能调度（Auto-Pilot）配置。
//
// LLM 决策引擎后端：三者齐全（非空）时，LLM 引擎调用真实大模型做语义推理；
// 任一为空则回退确定性 stub（仍可产出可执行动作，但非真 LLM）。
// 此处为可选配置，不参与 validate 的强制校验——stub 是合法默认。
type AutoPilotConfig struct {
	// LLMBaseURL 网关自身转发地址或任意 OpenAI 兼容端点（如 http://localhost:8787/v1）
	LLMBaseURL string
	// LLMAPIKey 下游凭证（sk-nv-xxx，需先在管理面板签发）
	LLMAPIKey string
	// LLMModel 目标模型，如 meta/llama-3.1-8b-instruct
	LLMModel string
}

// LLMConfigured 三者齐全则返回 true（启用真 LLM 后端）。
func (a AutoPilotConfig) LLMConfigured() bool {
	return a.LLMBaseURL != "" && a.LLMAPIKey != "" && a.LLMModel != ""
}

// BackupConfig 数据库备份配置（v0.8 新增）。
//
// 用 SQLite VACUUM INTO 产出事务一致快照（WAL 安全），按保留数轮转删最旧。
// 调度仿 modelmeta.Syncer：后台 ticker + 可被 admin API 即时触发。
type BackupConfig struct {
	// Dir 备份存放目录（容器内建议挂载到持久卷，如 /data/backups）
	Dir string
	// Interval 自动备份间隔（默认 24h）；<=0 则禁用定时备份，仅手动
	Interval time.Duration
	// Retention 保留最近多少份（默认 7）；<=0 则不自动清理
	Retention int
}

// ModelHealthConfig 模型级健康扫描与熔断配置（v0.9 新增）。
//
// 后台周期性对每个模型按其能力遍历所有 NVIDIA 推理接口各探 ProbeCount 次，
// 间隔 ProbeInterval；按成功率判定 closed/open/permanent。
// 成功率 < SuccessRateFloor 且连续 BadSweepToPermanent 次扫描 → 永久熔断。
type ModelHealthConfig struct {
	// ProbeCount 每个接口每轮探测次数（默认 3）
	ProbeCount int
	// ProbeInterval 同轮探测之间的间隔（默认 2s，避免突发）
	ProbeInterval time.Duration
	// SweepInterval 全量扫描周期（默认 30m）
	SweepInterval time.Duration
	// SuccessRateFloor 永久熔断地板：低于此成功率计为一次坏扫描（默认 30）
	SuccessRateFloor int
	// SuccessRateThreshold 临时熔断阈值：低于此但≥Floor 转 open（默认 80）
	SuccessRateThreshold int
	// BadSweepToPermanent 连续坏扫描次数达此值 → 永久熔断（默认 3）
	BadSweepToPermanent int
	// CooldownBase open 态初始冷却（指数退避起始，上限 10min；默认 30s）
	CooldownBase time.Duration
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (*Config, error) {
	// .env 可选（容器/裸机可能只用环境变量）
	_ = godotenv.Load()

		cfg := &Config{
			Server: ServerConfig{
				Port: envInt("PORT", 8787),
				// 默认初始令牌 "ADMIN"：首次登录后强制修改并写入 bcrypt 哈希。
				// 改密前所有受保护的管理 API 一律拒绝（见 admin.AuthMiddleware 锁定）。
				AdminToken: envStr("ADMIN_TOKEN", DefaultAdminToken),
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
		AutoPilot: AutoPilotConfig{
			LLMBaseURL: envStr("LLM_BASE_URL", ""),
			LLMAPIKey:  envStr("LLM_API_KEY", ""),
			LLMModel:   envStr("LLM_MODEL", ""),
		},
		Backup: BackupConfig{
			Dir:       envStr("BACKUP_DIR", "data/backups"),
			Interval:  envDuration("BACKUP_INTERVAL", 24*time.Hour),
			Retention: envInt("BACKUP_RETENTION", 7),
		},
		ModelHealth: ModelHealthConfig{
			ProbeCount:           envInt("MODEL_HEALTH_PROBE_COUNT", 3),
			ProbeInterval:        envDuration("MODEL_HEALTH_PROBE_INTERVAL", 2*time.Second),
			SweepInterval:        envDuration("MODEL_HEALTH_SWEEP_INTERVAL", 30*time.Minute),
			SuccessRateFloor:     envInt("MODEL_HEALTH_SUCCESS_RATE_FLOOR", 30),
			SuccessRateThreshold: envInt("MODEL_HEALTH_SUCCESS_RATE_THRESHOLD", 80),
			BadSweepToPermanent:  envInt("MODEL_HEALTH_BAD_SWEEP_TO_PERMANENT", 3),
			CooldownBase:         envDuration("MODEL_HEALTH_COOLDOWN_BASE", 30*time.Second),
		},
		Logging: LoggingConfig{
			Level:  envStr("LOG_LEVEL", "info"),
			Format: envStr("LOG_FORMAT", "json"),
		},
		Hooks: HooksConfig{
			WebhookURL:    envStr("WEBHOOK_URL", ""),
			WebhookSecret: envStr("WEBHOOK_SECRET", ""),
			WebhookEvents: envStr("WEBHOOK_EVENTS", ""),
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
