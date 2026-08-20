package log

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Logger 自定义日志器，包装 slog.Logger 提供结构化日志能力。
type Logger struct {
	inner *slog.Logger
}

func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, args...)
}
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, args...)
}
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, args...)
}
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, args...)
}

// With 返回一个带预设字段的 Logger。
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// NewLogger 创建一个全新的 Logger，拥有独立的 handler 实例。
func NewLogger() *Logger {
	return &Logger{inner: slog.New(newHandler())}
}

var (
	//defaultWriter     = &dynamicMultiWriter{writers: []io.Writer{os.Stderr}}
	defaultWriter     = &dynamicMultiWriter{writers: []io.Writer{}}
	registeredFilesMu sync.Mutex
	registeredFiles   map[string]bool
	globalLevel       = &slog.LevelVar{} // 初始为 LevelInfo
	fileLogDisabled   bool               // 配置目录 slog.json（~/.tinyclue/config/slog.json）中 enable=false 时阻止全局 SetFileLog
)

var std *Logger

func init() {
	std = NewLogger()
	loadFileConfig()
}

// SetFileLog 开启文件日志输出（额外写入 os.Stderr），支持按大小滚动。
// logPath: 日志目录；fileName: 日志文件名；maxSize: 单文件最大字节数；backups: 保留的滚动文件份数。
// 同一 logPath+fileName 重复调用会自动跳过，不会重复注册。
// 如果配置目录 slog.json（~/.tinyclue/config/slog.json）中设置了 enable:false，SetFileLog 全局失效。
func SetFileLog(logPath, fileName string, maxSize int64, backups int) error {
	if fileLogDisabled {
		return nil
	}
	fullPath := filepath.Join(logPath, fileName)

	registeredFilesMu.Lock()
	if registeredFiles == nil {
		registeredFiles = make(map[string]bool)
	}
	if registeredFiles[fullPath] {
		registeredFilesMu.Unlock()
		return nil
	}
	registeredFiles[fullPath] = true
	registeredFilesMu.Unlock()

	w, err := NewRotatingFileWriter(fullPath, maxSize, backups)
	if err != nil {
		return err
	}
	defaultWriter.Add(w)
	return nil
}

// SetLevel 设置全局日志级别，影响所有 Logger 实例。
// level: slog.LevelDebug / LevelInfo / LevelWarn / LevelError。
func SetLevel(level slog.Level) {
	globalLevel.Set(level)
}

// ParseLevel 将字符串解析为 slog.Level，支持 "debug"/"info"/"warn"/"error"（大小写不敏感）。
func ParseLevel(s string) (slog.Level, error) {
	switch {
	case caseEqual(s, "debug"):
		return slog.LevelDebug, nil
	case caseEqual(s, "info"):
		return slog.LevelInfo, nil
	case caseEqual(s, "warn"):
		return slog.LevelWarn, nil
	case caseEqual(s, "error"):
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %q", s)
	}
}

func caseEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i]|0x20 != b[i]|0x20 { // ASCII 小写化后比较
			return false
		}
	}
	return true
}

// ── Handler ──

// handler 实现 slog.Handler，输出自定义格式：
//
//	[{Level} {Time} {TraceID} {FileLine} {Hostname}:{PID}] [{Data}] {Message}
type handler struct {
	mu       sync.Mutex
	opts     HandlerOptions
	hostname string
	pid      int
	attrs    []slog.Attr // 来自 With/WithAttrs 的预设字段
	w        io.Writer
}

type HandlerOptions struct {
	Level slog.Leveler
}

func newHandler() *handler {
	host, _ := os.Hostname()
	return &handler{
		opts:     HandlerOptions{Level: globalLevel},
		hostname: host,
		pid:      os.Getpid(),
		w:        defaultWriter,
	}
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Level
	level := r.Level.String()

	// Time
	timeStr := r.Time.Format("2006-01-02 15:04:05.000")

	// TraceID — 从 ctx 读取 x-traceid
	traceID := "-"
	if v := ctx.Value("x-traceid"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			traceID = s
		}
	}

	// FileLine — 遍历调用栈，找到 log 包外部的真实调用方
	fileLine := "-"
	{
		pcs := make([]uintptr, 10)
		n := runtime.Callers(4, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, ok := frames.Next()
			if !ok {
				break
			}
			if filepath.Base(f.File) != "log.go" && filepath.Base(f.File) != "exported.go" {
				fileLine = fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
				break
			}
		}
	}

	// 收集结构化数据：预设字段 + 本次调用字段
	all := make(map[string]any, len(h.attrs)+5)
	for _, a := range h.attrs {
		all[a.Key] = resolveValue(a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		all[a.Key] = resolveValue(a.Value)
		return true
	})

	// 拼接输出
	fmt.Fprintf(h.w, "[%s %s %s %s %s:%d] %s", level, timeStr, traceID, fileLine, h.hostname, h.pid, r.Message)

	if len(all) > 0 {
		jsonData, _ := json.Marshal(all)
		fmt.Fprintf(h.w, " [%s]", jsonData)
	}
	fmt.Fprintln(h.w)
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := h.clone()
	h2.attrs = append(h2.attrs, attrs...)
	return h2
}

func (h *handler) WithGroup(name string) slog.Handler {
	h2 := h.clone()
	return h2
}

func (h *handler) clone() *handler {
	return &handler{
		opts:     h.opts,
		hostname: h.hostname,
		pid:      h.pid,
		attrs:    append([]slog.Attr(nil), h.attrs...),
		w:        h.w,
	}
}

// resolveValue 将 slog.Value 转换为可 JSON 序列化的值。
func resolveValue(v slog.Value) any {
	x := v.Any()
	if err, ok := x.(error); ok {
		return err.Error()
	}
	return x
}
