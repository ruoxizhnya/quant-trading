package main

// K8（TASKS K8 ③.4）：POST /api/signals 写入门的契约测试。
//
// 覆盖写入门的 fail-loud 口径：全批校验先于落库（单条坏 → 400 整批拒绝、
// 带索引）、未来 as_of 拒绝（DayOf 口径）、空批 400、store 缺失 503、
// 正常批 201 + accepted 计数。「写入 → 消费下单」的端到端在
// signals_e2e_test.go。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func newSignalsTestRouter(store strategy.SignalStore) *gin.Engine {
	// gin 的 mode 由 TestMain 设一次（AUD-12）—— 这里不调 gin.SetMode。
	router := gin.New()
	NewSignalsHandler(store, zerolog.Nop()).RegisterRoutes(router)
	return router
}

func postSignals(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/signals", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestSignalsWriteDoor_AcceptsBatch(t *testing.T) {
	store := &memorySignalStore{}
	router := newSignalsTestRouter(store)

	// as_of 取过去日期（2024-01-10 / 2024-01-15），写入门只拒未来。
	w := postSignals(t, router, `{"signals":[
		{"model_id":"ml-model","symbol":"600000.SH","direction":"long","strength":0.8,"as_of":"2024-01-10T00:00:00Z"},
		{"model_id":"ml-model","symbol":"600000.SH","direction":"close","strength":0.5,"as_of":"2024-01-15T00:00:00Z"}
	]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("合法批应 201，got %d body=%s", w.Code, w.Body.String())
	}
	var resp createSignalsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解码失败：%v", err)
	}
	if resp.Accepted != 2 {
		t.Fatalf("accepted 计数漂移：%d, want 2", resp.Accepted)
	}
	if len(store.signals) != 2 {
		t.Fatalf("落库条数漂移：%d, want 2", len(store.signals))
	}
}

func TestSignalsWriteDoor_RejectsBadItemIndexed(t *testing.T) {
	store := &memorySignalStore{}
	router := newSignalsTestRouter(store)

	// signals[1] direction=hold 非法 —— 整批拒绝且索引可见，不落任何行
	//（全批校验先于落库，无半批脏写）。
	w := postSignals(t, router, `{"signals":[
		{"model_id":"ml-model","symbol":"600000.SH","direction":"long","strength":0.8,"as_of":"2024-01-10T00:00:00Z"},
		{"model_id":"ml-model","symbol":"600000.SH","direction":"hold","strength":0.5,"as_of":"2024-01-15T00:00:00Z"}
	]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏条目应 400，got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "signals[1]") {
		t.Fatalf("错误消息应带索引 signals[1]，got %s", w.Body.String())
	}
	if len(store.signals) != 0 {
		t.Fatalf("坏批不得落库任何行，got %d", len(store.signals))
	}
}

func TestSignalsWriteDoor_RejectsFutureAsOf(t *testing.T) {
	store := &memorySignalStore{}
	router := newSignalsTestRouter(store)

	future := time.Now().AddDate(0, 0, 1).Format(time.RFC3339)
	w := postSignals(t, router, `{"signals":[
		{"model_id":"ml-model","symbol":"600000.SH","direction":"long","strength":0.8,"as_of":"`+future+`"}
	]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未来 as_of 应 400，got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "signals[0]") {
		t.Fatalf("错误消息应带索引 signals[0]，got %s", w.Body.String())
	}
	if len(store.signals) != 0 {
		t.Fatalf("未来信号不得落库，got %d", len(store.signals))
	}
}

func TestSignalsWriteDoor_RejectsEmptyBatch(t *testing.T) {
	router := newSignalsTestRouter(&memorySignalStore{})
	w := postSignals(t, router, `{"signals":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空批应 400，got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSignalsWriteDoor_StoreUnavailable(t *testing.T) {
	// store 缺失（未接库）时路由照常注册、请求时 503 —— 不 panic 不 404。
	router := newSignalsTestRouter(nil)
	w := postSignals(t, router, `{"signals":[
		{"model_id":"ml-model","symbol":"600000.SH","direction":"long","strength":0.8,"as_of":"2024-01-10T00:00:00Z"}
	]}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("store 缺失应 503，got %d body=%s", w.Code, w.Body.String())
	}
}
