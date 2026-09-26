package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

func newRecorder(t *testing.T, h gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	// gin 的 mode 由 TestMain 设一次（AUD-28）—— 别在这里调 gin.SetMode。
	r := gin.New()
	r.GET("/x", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w
}

func bodyOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是 JSON: %v (%s)", err, w.Body.String())
	}
	return got
}

// 形状必须保持 {"error": ...} —— 前端一直在读这个字段（client.ts），
// 统一出口的意义是加东西，不是换掉东西。
func TestFail_KeepsErrorField(t *testing.T) {
	t.Parallel()
	w := newRecorder(t, func(c *gin.Context) {
		Fail(c, http.StatusNotFound, "order not found")
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码应为 404，实际 %d", w.Code)
	}
	got := bodyOf(t, w)
	if got["error"] != "order not found" {
		t.Fatalf("error 字段应保持原样，实际 %v", got)
	}
	if got["code"] != CodeNotFound {
		t.Fatalf("code 应为 not_found，实际 %v", got["code"])
	}
}

// 这一条是 P1-5 真正的价值：**5xx 不再把内部错误原文吐给客户端**。
// 之前是 300 多处 `gin.H{"error": err.Error()}`，DB 报错、连接串都出去了。
func TestError_MasksInternalDetails(t *testing.T) {
	t.Parallel()
	const secret = "postgres://user:pa55w0rd@db:5432/quant"
	w := newRecorder(t, func(c *gin.Context) {
		Error(c, http.StatusInternalServerError, errors.New(secret+" connection refused"))
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码应为 500，实际 %d", w.Code)
	}
	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("内部错误原文泄露到了响应体：%s", w.Body.String())
	}
	got := bodyOf(t, w)
	if got["error"] != genericMessage(http.StatusInternalServerError) {
		t.Fatalf("5xx 应给通用文案，实际 %v", got["error"])
	}
	if got["code"] != CodeInternal {
		t.Fatalf("code 应为 internal，实际 %v", got["code"])
	}
}

// 4xx 的 err 文案本来就是给人看的（"参数不对"这类），照原样回。
func TestError_KeepsClientFacingMessage(t *testing.T) {
	t.Parallel()
	w := newRecorder(t, func(c *gin.Context) {
		Error(c, http.StatusBadRequest, errors.New("invalid date format, use YYYYMMDD"))
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d", w.Code)
	}
	got := bodyOf(t, w)
	if got["error"] != "invalid date format, use YYYYMMDD" {
		t.Fatalf("4xx 应保留原文，实际 %v", got["error"])
	}
}

func TestWrap_PrefixPlusLoggedCause(t *testing.T) {
	t.Parallel()
	w := newRecorder(t, func(c *gin.Context) {
		Wrap(c, http.StatusBadRequest, errors.New("boom"), "invalid request: ")
	})
	got := bodyOf(t, w)
	if got["error"] != "invalid request: boom" {
		t.Fatalf("前缀应拼上，实际 %v", got["error"])
	}
}

func TestFailf_Formats(t *testing.T) {
	t.Parallel()
	w := newRecorder(t, func(c *gin.Context) {
		Failf(c, http.StatusBadRequest, "invalid factor type: %s", "moon_phase")
	})
	got := bodyOf(t, w)
	if got["error"] != "invalid factor type: moon_phase" {
		t.Fatalf("格式化结果不对：%v", got["error"])
	}
}

// 兜底中间件：只接住「写了 c.Error 但没写响应」的情况，
// 已经写过响应的不能被它覆盖 —— 覆盖会静默改写 handler 的意图。
func TestErrorMiddleware_RendersUnwritten(t *testing.T) {
	t.Parallel()
	r := gin.New()
	r.Use(ErrorMiddleware())
	r.GET("/unwritten", func(c *gin.Context) {
		c.Error(errors.New("something broke"))
	})
	r.GET("/written", func(c *gin.Context) {
		c.Error(errors.New("something broke"))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/unwritten", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("没写响应的应被兜底成 500，实际 %d", w.Code)
	}
	if bodyOf(t, w)["code"] != CodeInternal {
		t.Fatalf("应渲染成 internal 错误，实际 %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/written", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("已写响应的不该被覆盖，实际 %d", w2.Code)
	}
}

// AppError 走同一出口。
func TestErrorMiddleware_AppErrorKeepsStatus(t *testing.T) {
	t.Parallel()
	r := gin.New()
	r.Use(ErrorMiddleware())
	r.GET("/x", func(c *gin.Context) {
		c.Error(&AppError{Status: http.StatusConflict, Code: CodeConflict, Message: "already running"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("应保留 409，实际 %d", w.Code)
	}
	if bodyOf(t, w)["error"] != "already running" {
		t.Fatalf("文案应保留：%s", w.Body.String())
	}
}

// AUD-60：把 pkg/errors 的**类别**映射成 HTTP 状态。
//
// 为什么值得单测：本包的 codeForStatus 是「状态码 → 错误码」单向派生，
// 所以「回哪个状态」完全由调用点决定。回测 handler 原先一律回 500，于是
// 「库里还没同步交易日历」这种调用方自己能解的前置问题，对客户端说成
// 「服务器内部错误」、对服务端自己记成 log.Error（污染告警）。
func TestStatusForAppError_MapsCategoryToStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		code apperrors.ErrorCode
		want int
		why  string
	}{
		{"请求不合法该调用方改", apperrors.ErrCodeInvalidInput, http.StatusBadRequest, "400"},
		{"点名资源不存在", apperrors.ErrCodeNotFound, http.StatusNotFound, "404"},
		{"请求没错但数据前置没满足", apperrors.ErrCodeDataQuality, http.StatusConflict, "409"},
		{"与当前资源状态冲突", apperrors.ErrCodeConflict, http.StatusConflict, "409"},
		{"权限不足", apperrors.ErrCodePermission, http.StatusForbidden, "403"},
		{"限流", apperrors.ErrCodeRateLimit, http.StatusTooManyRequests, "429"},
		{"超时", apperrors.ErrCodeTimeout, http.StatusGatewayTimeout, "504"},
		{"上游不可用", apperrors.ErrCodeUnavailable, http.StatusServiceUnavailable, "503"},
		{"未分类的内部错误", apperrors.ErrCodeInternal, http.StatusInternalServerError, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := apperrors.New(tc.code, "boom")
			if got := StatusForAppError(err); got != tc.want {
				t.Fatalf("StatusForAppError(%s) = %d, want %d（%s）", tc.code, got, tc.want, tc.why)
			}
		})
	}
}

// 对照腿：**未知即服务端问题**。没有这条，「映射表把 400 给了一个内部 bug」
// 这种反向错误永远不会被发现 —— 而它比原来的「一律 500」更糟：它会把
// 服务端的 bug 说成「你请求错了」，调用方于是去改一个本来就对的请求。
func TestStatusForAppError_UnknownStaysServerSide(t *testing.T) {
	t.Parallel()

	if got := StatusForAppError(errors.New("connection reset by peer")); got != http.StatusInternalServerError {
		t.Fatalf("非 AppError 的错误必须留 500，实际 %d", got)
	}
	if got := StatusForAppError(nil); got != http.StatusInternalServerError {
		t.Fatalf("nil 必须留 500，实际 %d", got)
	}
	// 引擎常见的形状是「AppError 包着底层 cause」，errors.As 必须能穿过去。
	wrapped := apperrors.Wrap(errors.New("pgx: no rows"), apperrors.ErrCodeDataQuality, "calendar missing", "RunBacktest")
	if got := StatusForAppError(wrapped); got != http.StatusConflict {
		t.Fatalf("带 cause 的 AppError 应仍是 409，实际 %d", got)
	}
}
