// Package httpx 提供统一的 HTTP 响应封装。
//
// 目标：
//   - 所有成功响应都是 {code, message, data} 三段式
//   - 失败响应也是同样的形状，方便前端统一处理
//   - 不引入任何抽象（no interface）——Day 1 阶段 helper 就够
package httpx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 业务错误码。0 表示成功。
const (
	CodeOK               = 0
	CodeBadRequest       = 40000
	CodeNotFound         = 40400
	CodeInternal         = 50000
	CodeRedisUnavailable = 50301
	CodeVoucherNotStart  = 60001
	CodeVoucherEnded     = 60002
	CodeOutOfStock       = 60003
	CodeAlreadyBought    = 60004
)

// AppError 是业务层定义的错误，handler 可以判断 Code 决定 HTTP 状态。
type AppError struct {
	Code    int
	Message string
	Err     error // 内部错误，仅记日志，不直接暴露给前端
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

// 便捷构造
func NewBadRequest(msg string) *AppError { return &AppError{Code: CodeBadRequest, Message: msg} }
func NewNotFound(msg string) *AppError   { return &AppError{Code: CodeNotFound, Message: msg} }
func NewInternal(err error) *AppError {
	return &AppError{Code: CodeInternal, Message: "internal error", Err: err}
}
func NewRedisUnavailable(err error) *AppError {
	return &AppError{Code: CodeRedisUnavailable, Message: "redis unavailable", Err: err}
}

// OK 写一个 200 + 业务码 0 的响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{
		"code":    CodeOK,
		"message": "ok",
		"data":    data,
	})
}

// Fail 把 error 转换成 HTTP 响应。
// 已知 AppError 用其 Code 决定 HTTP 状态；未知 err 当 500。
func Fail(c *gin.Context, err error) {
	if appErr, ok := errors.AsType[*AppError](err); ok {
		httpStatus := http.StatusOK // 业务错误仍用 200，code 字段标识
		switch appErr.Code / 100 {
		case 400:
			httpStatus = http.StatusBadRequest
		case 404:
			httpStatus = http.StatusNotFound
		case 500:
			httpStatus = http.StatusInternalServerError
		case 503:
			httpStatus = http.StatusServiceUnavailable
		}
		c.JSON(httpStatus, gin.H{
			"code":    appErr.Code,
			"message": appErr.Message,
		})
		return
	}
	// 兜底：未知错误
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    CodeInternal,
		"message": "internal error",
	})
}
