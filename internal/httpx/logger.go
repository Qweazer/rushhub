// Package httpx 提供 ctx-aware 的 slog logger。
//
// 目的：业务层只要用 FromContext(ctx) 拿 logger，
// 每条日志都会自动带上 request_id，不用每次手动 slog.String(...)。
package httpx

import (
	"context"
	"log/slog"
	"os"

	"gorush/internal/middleware"
)

// Init 初始化 slog：JSON handler 输出到 stderr。
// 后续接 ELK / Loki 只需换 Handler，不必改业务代码。
func Init() {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(h))
}

// FromContext 返回一个绑定了 request_id 的 logger。
//   - 没有 request_id 时返回 slog.Default()
//
// 用法：
//
//	httpx.FromContext(ctx).Info("seckill attempt",
//	    slog.Uint64("user_id", uid),
//	    slog.Uint64("voucher_id", vid),
//	)
func FromContext(ctx context.Context) *slog.Logger {
	rid := middleware.RequestIDFromContext(ctx)
	if rid == "" {
		return slog.Default()
	}
	return slog.Default().With(slog.String("request_id", rid))
}
