package main

// DATA-06：POST /sync/index-constituents/:index_code 在落库失败时不得报成功。
//
// 旧形态：落库发生在 TushareClient 内部，error 只 Warn；handler 拿到非空切片
// 就回 200 "index constituents synced successfully" —— HTTP 面上「一行没写进去」
// 与「写成功」不可区分。本文件钉住修复后的契约（工单 ③ 选 a1：错误上抛 ⇒ 5xx）：
//   1. 落库失败 ⇒ 5xx，响应体不得出现 "synced successfully"，原始错误可追溯。
//   2. 落库成功 ⇒ 200，且「取到 N 条」与「落库 N 条」是响应体里两个可区分的数。
//   3. 400 校验（不支持的指数代码）行为不变 —— 修复不许顺带动边界。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

const indexConstituentsStubBody = `{"code":0,"msg":"","data":{"fields":["index_code","con_code","trade_date","weight"],"items":[
	["000300.SH","600000.SH","20241031",1.5],
	["000300.SH","600036.SH","20241031",2.5],
	["000300.SH","600036.SH","20241129",2.7]
]}}`

// failingIdxStore / okIdxStore 经 TushareStore 接口注入（生产码不为测试让路：
// handler 收的就是 normal 构造路径出来的 *data.TushareClient）。
type failingIdxStore struct {
	data.TushareStore
}

func (s *failingIdxStore) SaveIndexConstituentBatch(_ context.Context, _ []*domain.IndexConstituent) error {
	return errors.New("boom: index_constituents unreachable")
}

func (s *failingIdxStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error { return nil }

type okIdxStore struct {
	data.TushareStore
	saved int
}

func (s *okIdxStore) SaveIndexConstituentBatch(_ context.Context, rows []*domain.IndexConstituent) error {
	s.saved += len(rows)
	return nil
}

func (s *okIdxStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error { return nil }

func newSyncIdxRouter(t *testing.T, store data.TushareStore) *gin.Engine {
	t.Helper()
	// stub 与 main.go:139 用同一个路由形状注册，钉住 :index_code 参数名。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(indexConstituentsStubBody))
	}))
	t.Cleanup(srv.Close)

	tc := data.NewTushareClient("test-token", srv.URL, 0, store, nil)
	router := gin.New()
	router.POST("/sync/index-constituents/:index_code", syncIndexConstituentsHandler(tc))
	return router
}

// TestSyncIndexConstituents_PersistFailureNotReportedSuccess 是 DATA-06 的红腿：
// 生产码把 error 重新吞回 Warn ⇒ 本测试红（200 + "synced successfully"）。
func TestSyncIndexConstituents_PersistFailureNotReportedSuccess(t *testing.T) {
	router := newSyncIdxRouter(t, &failingIdxStore{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sync/index-constituents/000300.SH", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"落库失败不得回 2xx —— 请求要做的正是「取到并落库」")
	assert.NotContains(t, w.Body.String(), "synced successfully",
		"失败响应里不得出现成功文案")

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	// 失败环节对外可见（静态文案），内部原因按本仓 5xx 口径不外泄。
	assert.Contains(t, w.Body.String(), "failed to persist",
		"响应体必须说清失败发生在落库环节，不是通用内部错误")
	assert.NotContains(t, w.Body.String(), "unreachable",
		"内部错误细节（连接串/SQL 这类）不得进响应体")
	assert.NotContains(t, body, "count",
		"失败响应不得再挂一个看起来像成功计数的 count 字段")
}

// TestSyncIndexConstituents_SuccessDistinguishesFetchedAndPersisted 是反证腿：
// 同一 stub 返回成功时读数照常，且两个数在响应体里可区分。
func TestSyncIndexConstituents_SuccessDistinguishesFetchedAndPersisted(t *testing.T) {
	store := &okIdxStore{}
	router := newSyncIdxRouter(t, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sync/index-constituents/000300.SH", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Message        string `json:"message"`
		IndexCode      string `json:"index_code"`
		FetchedCount   int    `json:"fetched_count"`
		PersistedCount int    `json:"persisted_count"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	assert.Equal(t, "index constituents synced successfully", body.Message)
	assert.Equal(t, "000300.SH", body.IndexCode)
	assert.Equal(t, 2, body.FetchedCount, "取到 2 只（stub 两期快照去重后）")
	assert.Equal(t, 2, body.PersistedCount, "落库 2 条")
	assert.Equal(t, 2, store.saved, "store 实际收到 2 行（persisted_count 不是自说自话）")
}

// TestSyncIndexConstituents_RejectsUnsupportedIndex 固定边界：400 校验不受本单影响。
func TestSyncIndexConstituents_RejectsUnsupportedIndex(t *testing.T) {
	router := newSyncIdxRouter(t, &okIdxStore{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sync/index-constituents/999999.SH", nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported index code")
}
