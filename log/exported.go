package log

import (
	"context"
	"fmt"
)

// 顶层便捷函数

func Debug(ctx context.Context, msg string, args ...any) { std.Debug(ctx, msg, args...) }
func Info(ctx context.Context, msg string, args ...any)  { std.Info(ctx, msg, args...) }
func Warn(ctx context.Context, msg string, args ...any)  { std.Warn(ctx, msg, args...) }
func Error(ctx context.Context, msg string, args ...any) { std.Error(ctx, msg, args...) }

func Debugf(ctx context.Context, format string, args ...any) {
	std.Debug(ctx, fmt.Sprintf(format, args...))
}
func Infof(ctx context.Context, format string, args ...any) {
	std.Info(ctx, fmt.Sprintf(format, args...))
}
func Warnf(ctx context.Context, format string, args ...any) {
	std.Warn(ctx, fmt.Sprintf(format, args...))
}
func Errorf(ctx context.Context, format string, args ...any) {
	std.Error(ctx, fmt.Sprintf(format, args...))
}

// New 创建一个新的 Logger。
func New() *Logger {
	return NewLogger()
}
