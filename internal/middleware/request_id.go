package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// ctxKey 类型见 auth.go。
const requestIDKey ctxKey = iota + 100 // 用 100+ 避开 userIDKey 的 1

// requestIDHeader 是 HTTP 响应里带回的 header 名，方便客户端/网关关联。
const requestIDHeader = "X-Request-ID"

// newRequestID 生成 16 字节随机 ID（32 个 hex 字符）。不依赖第三方包。
func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestID 给每个请求分配/透传 request_id，写进 ctx 并加到 slog 默认 logger。
//
// 如果请求带了 X-Request-ID header 就复用（方便跨服务追踪）；
// 否则生成一个新的。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = newRequestID()
		}
		// 1) 写到 ctx 让业务层拿
		ctx := context.WithValue(c.Request.Context(), requestIDKey, rid)
		c.Request = c.Request.WithContext(ctx)

		// 2) 写到 gin.Context 备用
		c.Set("request_id", rid)

		// 3) 写到响应头，客户端也能看到
		c.Writer.Header().Set(requestIDHeader, rid)

		c.Next()
	}
}

// RequestIDFromContext 从 ctx 取 request_id。
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// AccessLog 打印每条请求的 method/path/status/latency，并携带 request_id。
// 替换 gin.Default() 自带的 Logger 中间件，避免日志格式不可控。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		rid := RequestIDFromContext(c.Request.Context())
		latency := time.Since(start)

		slog.Info("http",
			slog.String("request_id", rid),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("latency", latency),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}

// Recovery 自定义 panic 恢复：返回 JSON + 打印带 request_id 的错误日志。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				rid := RequestIDFromContext(c.Request.Context())
				slog.Error("panic recovered",
					slog.String("request_id", rid),
					slog.Any("panic", r),
					slog.String("path", c.Request.URL.Path),
				)
				c.AbortWithStatusJSON(500, gin.H{
					"code":       50000,
					"message":    "internal server error",
					"request_id": rid,
				})
			}
		}()
		c.Next()
	}
}
