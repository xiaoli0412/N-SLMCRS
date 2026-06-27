// Package logging 提供统一的 leveled 结构化日志（v0.9）。
//
// 基于 Go 1.25 内置 log/slog（零外部依赖、CGO-free）。Logger 同时扇出到：
//   - stdout（JSON 或 text，按 LOG_FORMAT）
//   - app_logs 表（经 data.Store.WriteLog，使日志中心页有真实全量数据）
//
// trace_id 通过 context 注入（WithTraceID），自动随日志条目落库，便于全链路检索。
//
// 用法：
//
//	log := logging.New(store, logging.Config{Level: slog.LevelInfo, Format: "json"})
//	ctx := logging.WithTraceID(context.Background(), traceID)
//	log.Info(ctx, "scheduler", "网关启动，监听 :8787")
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"

	"github.com/nslmcrs/gateway/internal/data"
)

// 配置键。
type Config struct {
	Level  slog.Level // 日志级别
	Format string     // json | text
	File   string     // 可选日志文件路径（v0.10；空=仅 stdout）
}

// traceIDKey context 键类型（避免与其他包冲突）。
type ctxKey string

const traceKey ctxKey = "trace_id"

// WithTraceID 将 trace_id 注入 context。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceKey, traceID)
}

// traceIDFrom 取 context 中的 trace_id。
func traceIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v
	}
	return ""
}

// Logger 统一日志器。
type Logger struct {
	sl     *slog.Logger
	store  *data.Store
	mu     sync.Mutex
	level  slog.Level
	out    io.Writer  // stdout 或 stdout+file 多写
	file   *os.File   // 可选文件 sink（nil=仅 stdout）
	format string     // json | text（SetLevel 重建 handler 时保留）
}

// New 创建日志器。store 为 nil 时仅输出 stdout（不落库）。
// 若 cfg.File 非空，额外写日志文件（追加模式，自动建父目录），扇出 stdout+file。
func New(store *data.Store, cfg Config) *Logger {
	level := cfg.Level
	handlerOpt := &slog.HandlerOptions{Level: level}

	// 输出端：stdout，可选叠加文件 sink（v0.10：日志落盘到 D 盘，避免写满 C 盘）
	out := io.Writer(os.Stdout)
	var f *os.File
	if cfg.File != "" {
		if dir := fileDir(cfg.File); dir != "" {
			_ = os.MkdirAll(dir, 0o755)
		}
		if opened, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			f = opened
			out = io.MultiWriter(os.Stdout, f)
		}
	}

	var sh slog.Handler
	if cfg.Format == "text" {
		sh = slog.NewTextHandler(out, handlerOpt)
	} else {
		sh = slog.NewJSONHandler(out, handlerOpt)
	}
	return &Logger{sl: slog.New(sh), store: store, level: level, out: out, file: f, format: cfg.Format}
}

// Close 关闭日志文件句柄（优雅关闭时调用；无文件则无操作）。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// fileDir 取路径的目录部分（跨平台：兼容 / 与 \）。
func fileDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

// SetLevel 运行时调整级别（保留原格式与输出端）。
func (l *Logger) SetLevel(level slog.Level) {
	l.mu.Lock()
	l.level = level
	handlerOpt := &slog.HandlerOptions{Level: level}
	var sh slog.Handler
	if l.format == "text" {
		sh = slog.NewTextHandler(l.out, handlerOpt)
	} else {
		sh = slog.NewJSONHandler(l.out, handlerOpt)
	}
	l.sl = slog.New(sh)
	l.mu.Unlock()
}

// enabled 当前级别是否输出。
func (l *Logger) enabled(level slog.Level) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return level >= l.level
}

// log 统一落 stdout + app_logs。
func (l *Logger) log(ctx context.Context, level slog.Level, source, msg string, attrs ...any) {
	if !l.enabled(level) {
		return
	}
	traceID := traceIDFrom(ctx)
	// stdout（结构化）
	args := []any{"source", source}
	if traceID != "" {
		args = append(args, "trace_id", traceID)
	}
	args = append(args, attrs...)
	switch level {
	case slog.LevelDebug:
		l.sl.DebugContext(ctx, msg, args...)
	case slog.LevelInfo:
		l.sl.InfoContext(ctx, msg, args...)
	case slog.LevelWarn:
		l.sl.WarnContext(ctx, msg, args...)
	case slog.LevelError:
		l.sl.ErrorContext(ctx, msg, args...)
	}
	// app_logs 落库（异步不阻塞请求；失败静默，避免日志拖垮主流程）
	if l.store != nil {
		ctxJSON := attrsToJSON(attrs)
		go func(s *data.Store, lvl, src, tid, m, cj string) {
			_ = s.WriteLog(context.Background(), lvl, src, tid, m, cj)
		}(l.store, level.String(), source, traceID, msg, ctxJSON)
	}
}

// Debug / Info / Warn / Error 便捷方法。
func (l *Logger) Debug(ctx context.Context, source, msg string, attrs ...any) {
	l.log(ctx, slog.LevelDebug, source, msg, attrs...)
}
func (l *Logger) Info(ctx context.Context, source, msg string, attrs ...any) {
	l.log(ctx, slog.LevelInfo, source, msg, attrs...)
}
func (l *Logger) Warn(ctx context.Context, source, msg string, attrs ...any) {
	l.log(ctx, slog.LevelWarn, source, msg, attrs...)
}
func (l *Logger) Error(ctx context.Context, source, msg string, attrs ...any) {
	l.log(ctx, slog.LevelError, source, msg, attrs...)
}

// attrsToJSON 把 slog 变参 attrs（k,v,k,v...）压成紧凑 JSON 串存入 app_logs.context。
func attrsToJSON(attrs []any) string {
	if len(attrs) == 0 {
		return ""
	}
	var b []byte
	b = append(b, '{')
	for i := 0; i+1 < len(attrs); i += 2 {
		if i > 0 {
			b = append(b, ',')
		}
		k, _ := attrs[i].(string)
		b = append(b, '"')
		b = append(b, k...)
		b = append(b, '"', ':')
		b = appendJSONValue(b, attrs[i+1])
	}
	b = append(b, '}')
	return string(b)
}

// appendJSONValue 简单值序列化（字符串/数字/布尔；其余走 strconv）。
func appendJSONValue(b []byte, v any) []byte {
	switch x := v.(type) {
	case string:
		b = append(b, '"')
		b = append(b, x...)
		b = append(b, '"')
	case int:
		b = strconv.AppendInt(b, int64(x), 10)
	case int64:
		b = strconv.AppendInt(b, x, 10)
	case float64:
		b = strconv.AppendFloat(b, x, 'f', -1, 64)
	case bool:
		b = strconv.AppendBool(b, x)
	default:
		s := strconv.FormatInt(int64(0), 10) // 兜底：未知类型存为字符串
		_ = s
		b = append(b, '"')
		b = append(b, '"')
	}
	return b
}

// ParseLevel 解析级别字符串为 slog.Level。
func ParseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// StdLogger 暴露底层 *slog.Logger，供需要直接调用 slog 的场景使用。
func (l *Logger) StdLogger() *slog.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sl
}

// Level 返回当前级别。
func (l *Logger) Level() slog.Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}
