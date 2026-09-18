package httpserver

// P1-5：HTTP 错误响应的唯一出口。
//
// 此前全仓有 300 余处手写 `c.JSON(http.StatusX, gin.H{"error": ...})`，
// `c.Error()` 一次都没用过。三件事因此做不成：
//
//  1. 错误形状无法统一 —— 有的带 details，有的不带，前端只能靠 `error` 字段
//     勉强兜着，谁也不敢加字段。
//  2. **内部错误原文被直接吐给客户端** —— `gin.H{"error": err.Error()}`
//     在 5xx 上出现了一百多次，DB 报错、连接串、内部路径都在里面。
//     这类信息泄露就该在一处关掉，而不是指望 300 个调用点都记得。
//  3. 服务端日志里没有错误 —— 响应写出去就完了，没人记，事后无从查。
//
// 这里给两个东西：一组构造函数（表达"发生了什么"）和一个写出函数
// （决定"客户端看到什么、日志记什么"）。改口径只需改后者。

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// 错误码。前端可以按 code 分支，不必去匹配中文文案。
const (
	CodeBadRequest   = "bad_request"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeRateLimited  = "rate_limited"
	CodeUnavailable  = "unavailable"
	CodeInternal     = "internal"
)

// AppError 是服务内部向 HTTP 层传递的错误。
//
// Status / Code / Message 决定客户端看到什么；Cause 只进日志 ——
// 它常带着底层驱动的错误文本，那不是该给浏览器看的东西。
type AppError struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Cause }

// codeForStatus 给出状态码对应的默认错误码。
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	default:
		return CodeInternal
	}
}

// Fail 用给定文案回一个错误响应。
//
// 文案是**写给人看的**，会原样出现在响应体里 —— 别把内部细节拼进去。
func Fail(c *gin.Context, status int, message string) {
	writeError(c, status, codeForStatus(status), message, nil)
}

// Failf 是带格式化的 Fail。
func Failf(c *gin.Context, status int, format string, args ...any) {
	writeError(c, status, codeForStatus(status), fmt.Sprintf(format, args...), nil)
}

// Error 把一个 error 转成错误响应。
//
// 分水岭在 5xx：4xx 的 err 文案本来就是给用户看的（"参数不对"这类），
// 照原样回；**5xx 一律换成通用文案，真实原因只进日志** —— 那里面可能有
// 连接串、SQL、内部路径。
func Error(c *gin.Context, status int, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	if status >= http.StatusInternalServerError {
		writeError(c, status, CodeInternal, genericMessage(status), err)
		return
	}
	writeError(c, status, codeForStatus(status), err.Error(), err)
}

// Wrap 回一个带前缀的错误响应：message 给用户，err 进日志。
//
// 用于「这个操作失败了：xxx」这类场景 —— 前缀负责说清是什么操作，
// err 负责让服务端能查。5xx 时和 Error 一样收口成通用文案。
func Wrap(c *gin.Context, status int, err error, message string) {
	if err == nil {
		Fail(c, status, message)
		return
	}
	if status >= http.StatusInternalServerError {
		writeError(c, status, CodeInternal, genericMessage(status), err)
		return
	}
	writeError(c, status, codeForStatus(status), message+err.Error(), err)
}

// FailCause 回一个**开发者写死**的文案，把 cause 只留在日志里。
//
// 和 Wrap 的区别：Wrap 会把 err.Error() 拼进文案（4xx 场景，那是给用户看的
// 原因），FailCause 从不拼 —— 用于 5xx 上「既要给用户一个说清哪一步失败的
// 静态文案，又不能把内部原因吐出去」的情况。
func FailCause(c *gin.Context, status int, message string, cause error) {
	writeError(c, status, codeForStatus(status), message, cause)
}

// ErrorMiddleware 兜底渲染：处理那些调了 c.Error() 但没写响应的情况。
//
// 正常路径走上面的 Fail / Error，**不会**走到这里 —— 它只接住中间件或
// 未来代码里 `c.Error(err); return` 这种写法，避免又变回"错误被吞掉、
// 客户端收到一个空的 200"。
func ErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last()
		var appErr *AppError
		if errors.As(err.Err, &appErr) {
			writeError(c, appErr.Status, appErr.Code, appErr.Message, appErr.Cause)
			return
		}
		writeError(c, http.StatusInternalServerError, CodeInternal,
			genericMessage(http.StatusInternalServerError), err.Err)
	}
}

// writeError 是所有错误响应的唯一出口。
//
// 响应体保持 {"error": ..., "code": ...}：error 字段是前端一直在读的，
// 不能动；code 是新加的，给前端按类型分支用。
func writeError(c *gin.Context, status int, code, message string, cause error) {
	body := gin.H{"error": message, "code": code}
	if status >= http.StatusInternalServerError {
		// 细节只留服务端：客户端拿到的是 code 和一句人话。
		log.Error().Err(cause).Str("code", code).Int("status", status).
			Str("path", c.Request.URL.Path).
			Msg("handler error")
	} else if cause != nil {
		log.Debug().Err(cause).Str("code", code).Int("status", status).
			Str("path", c.Request.URL.Path).
			Msg("handler error")
	}
	c.JSON(status, body)
}

func genericMessage(status int) string {
	switch status {
	case http.StatusServiceUnavailable:
		return "服务暂时不可用"
	case http.StatusBadGateway:
		return "上游服务异常"
	case http.StatusGatewayTimeout:
		return "上游服务超时"
	default:
		return "服务器内部错误"
	}
}
