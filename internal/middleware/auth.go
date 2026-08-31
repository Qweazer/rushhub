// Package middleware 集中放 Gin 中间件。
//
// 设计原则：
//   - 中间件只做"上下文注入"——读 header → 放值；不直接做业务校验。
//   - 用 typed key 避免和其他包的 ctx 键冲突。
//   - 业务校验放到 service 层（"用户不存在"是业务问题，不是 HTTP 问题）。
package middleware

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ctxKey 是私有类型，避免和其它包的 ctx key 撞车。
type ctxKey int

const (
	userIDKey ctxKey = iota + 1 // 让 iota 从 1 开始，0 留作"未设置"哨兵
)

// Auth 模拟登录态：从 X-User-ID 头读取用户 id，写进 ctx。
// 缺失或非法 → 401。
//
// Day 1 暂不做"用户是否存在"的校验——Step 6 ~ 8 一直靠 seed 出来的 Alice/Bob/Carol。
// 真正登录态会在后续 Day 接 JWT。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("X-User-ID")
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    40100,
				"message": "missing X-User-ID",
			})
			return
		}
		uid, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || uid == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    40100,
				"message": "invalid X-User-ID",
			})
			return
		}
		// 同时塞进 gin.Context 和标准 ctx，让两层都能拿到。
		c.Set("user_id", uid)
		ctx := context.WithValue(c.Request.Context(), userIDKey, uid)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// UserIDFromContext 从标准 ctx 取 user id。
// 返回 (id, ok)；ok=false 表示当前 ctx 不在认证链路中。
func UserIDFromContext(ctx context.Context) (uint64, bool) {
	v := ctx.Value(userIDKey)
	if v == nil {
		return 0, false
	}
	id, ok := v.(uint64)
	return id, ok
}
