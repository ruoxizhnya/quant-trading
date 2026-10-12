package data

// 产业链导入面（P2-2）测试：normalize 纯函数 + 错误传播 + seed CSV。
//
// 错误传播是本文件的红线：ETL 路径上任何失败（HTTP 错、tushare 错误信封、
// 响应形状违约、Save 失败）都必须向上传播——不许 log 后 continue 返回 nil。
// 破坏验证②的 Red 腿指回这里的 TestFetch*Propagates* 三兄弟。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// ── 测试替身 ─────────────────────────────────────────────────────────────

// recordingSupplyChainStore 记录四张表的 Save 入参，不碰库。
// 嵌入 TushareStore（nil 接口）满足其余方法——本用例只走 Save* 与
// SaveRawIngest；误调其它方法会 panic，那正是想要的信号。
type recordingSupplyChainStore struct {
	TushareStore
	tree     []*domain.SWIndustryNode
	edges    []*domain.SupplyChainEdge
	vitality []*domain.IndustryVitality
	etf      []*domain.ETFConstituent
}

func (s *recordingSupplyChainStore) SaveIndustryTreeBatch(_ context.Context, rows []*domain.SWIndustryNode) error {
	s.tree = append(s.tree, rows...)
	return nil
}

func (s *recordingSupplyChainStore) SaveSupplyChainEdgeBatch(_ context.Context, rows []*domain.SupplyChainEdge) error {
	s.edges = append(s.edges, rows...)
	return nil
}

func (s *recordingSupplyChainStore) SaveIndustryVitalityBatch(_ context.Context, rows []*domain.IndustryVitality) error {
	s.vitality = append(s.vitality, rows...)
	return nil
}

func (s *recordingSupplyChainStore) SaveETFConstituentBatch(_ context.Context, rows []*domain.ETFConstituent) error {
	s.etf = append(s.etf, rows...)
	return nil
}

func (s *recordingSupplyChainStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// failingSupplyChainStore 全部 Save 返回同一个错误——用来证明调用方把错误
// 传上来了，而不是吞掉。
type failingSupplyChainStore struct {
	TushareStore
	err error
}

func (s *failingSupplyChainStore) SaveIndustryTreeBatch(_ context.Context, _ []*domain.SWIndustryNode) error {
	return s.err
}
func (s *failingSupplyChainStore) SaveSupplyChainEdgeBatch(_ context.Context, _ []*domain.SupplyChainEdge) error {
	return s.err
}
func (s *failingSupplyChainStore) SaveIndustryVitalityBatch(_ context.Context, _ []*domain.IndustryVitality) error {
	return s.err
}
func (s *failingSupplyChainStore) SaveETFConstituentBatch(_ context.Context, _ []*domain.ETFConstituent) error {
	return s.err
}
func (s *failingSupplyChainStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// rawIngestAlias 避开测试文件直接 import storage（与 tushare_raw_test.go 的
// 轻量替身同思路；真正签名是 *storage.RawIngest，用别名保持测试自足）。
type rawIngestAlias = struct{}

// stubTushare 起一个恒返回给定 body 的 tushare stub，返回其 URL。
func stubTushare(t *testing.T, body string, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newSupplyChainTestClient 指向 stub 的 TushareClient（maxRetries=0，失败即返）。
func newSupplyChainTestClient(baseURL string, store TushareStore) *TushareClient {
	return NewTushareClient("test-token", baseURL, 0, store, nil)
}

// ── normalize：index_classify ────────────────────────────────────────────

const indexClassifyBody = `{"code":0,"msg":"","data":{"fields":["index_code","industry_name","level","industry_code","parent_code","is_pub"],"items":[
	["801010.SI","农林牧渔","L1","801010","","Y"],
	["801012.SI","种植业","L2","801012","801010","Y"],
	["801014.SI","养殖业","L2","801014","801010","Y"],
	["801030.SI","基础化工","L1","801030","","Y"]
]}}`

func TestNormalizeSWIndustryClassify_MapsFieldsAndDerivesLeaves(t *testing.T) {
	resp := decodeTushareBody(t, indexClassifyBody)
	asOf := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

	nodes, err := normalizeSWIndustryClassify(resp, SupplyChainSourceTushare, asOf)
	require.NoError(t, err)
	require.Len(t, nodes, 4)

	require.Equal(t, "801010", nodes[0].Code, "code 应优先取 industry_code")
	require.Equal(t, "农林牧渔", nodes[0].Name)
	require.Equal(t, 1, nodes[0].Level, "L1 → 1")
	require.Empty(t, nodes[0].ParentCode, "一级行业父节点为空串")
	require.False(t, nodes[0].IsLeaf, "有子节点的不是叶子")

	require.Equal(t, "801012", nodes[1].Code)
	require.Equal(t, "801010", nodes[1].ParentCode)
	require.Equal(t, 2, nodes[1].Level, "L2 → 2")
	require.True(t, nodes[1].IsLeaf, "无子节点的是叶子")

	require.Equal(t, SupplyChainSourceTushare, nodes[2].Source)
	require.True(t, nodes[3].IsLeaf, "801030 无子节点，是叶子")
}

func TestNormalizeSWIndustryClassify_LevelAsBareNumber(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["index_code","industry_name","level","industry_code","parent_code"],"items":[["801010.SI","农林牧渔","3","801010","801000"]]}}`)
	nodes, err := normalizeSWIndustryClassify(resp, SupplyChainSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, 3, nodes[0].Level, "裸数字层级串也要解析")
}

func TestNormalizeSWIndustryClassify_RejectsBadLevel(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["index_code","industry_name","level","industry_code","parent_code"],"items":[["801010.SI","农林牧渔","一级","801010",""]]}}`)
	_, err := normalizeSWIndustryClassify(resp, SupplyChainSourceTushare, time.Now().UTC())
	require.Error(t, err, "level 不可解析必须报错（静默默认 1 会把三级错标成一级）")
	require.Contains(t, err.Error(), "level")
}

func TestNormalizeSWIndustryClassify_RejectsMissingKeyColumns(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["index_code","level"],"items":[["801010.SI","L1"]]}}`)
	_, err := normalizeSWIndustryClassify(resp, SupplyChainSourceTushare, time.Now().UTC())
	require.Error(t, err, "缺 industry_name 列 = 形状不符契约，必须报错")
}

// ── normalize：index_member ──────────────────────────────────────────────

const indexMemberBody = `{"code":0,"msg":"","data":{"fields":["index_code","con_code","in_date","out_date"],"items":[
	["801012.SI","600519.SH","20140101",""],
	["801012.SI","000998.SZ","20140101",""],
	["801012.SI","","20140101",""]
]}}`

func TestNormalizeSWIndustryMembers_MapsToEdges(t *testing.T) {
	resp := decodeTushareBody(t, indexMemberBody)
	edges, err := normalizeSWIndustryMembers(resp, "801012.SI", SupplyChainSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, edges, 2, "con_code 为空的行跳过")

	require.Equal(t, "600519.SH", edges[0].Upstream, "upstream=成分股")
	require.Equal(t, "801012.SI", edges[0].Downstream, "downstream=行业 code（取响应列）")
	require.Equal(t, domain.RelationIndustryMember, edges[0].Relation)
	require.InDelta(t, 0.0, edges[0].Weight, 1e-12, "index_member 无权重，weight 恒 0")
}

func TestNormalizeSWIndustryMembers_FallsBackToRequestedIndex(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["con_code"],"items":[["600519.SH"]]}}`)
	edges, err := normalizeSWIndustryMembers(resp, "801012.SI", SupplyChainSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "801012.SI", edges[0].Downstream, "响应无 index_code 列时退化用请求参数")
}

func TestNormalizeSWIndustryMembers_RejectsMissingConCode(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["index_code","in_date"],"items":[["801012.SI","20140101"]]}}`)
	_, err := normalizeSWIndustryMembers(resp, "801012.SI", SupplyChainSourceTushare, time.Now().UTC())
	require.Error(t, err)
}

// ── normalize：fund_portfolio ────────────────────────────────────────────

func TestNormalizeETFPortfolio_MapsFieldsAndPITFallback(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["ts_code","ann_date","end_date","symbol","mkv","amount","stk_mkv_ratio"],"items":[
	["510300.SH","20260720","20260630","600519.SH",123456.0,100.0,5.23],
	["510300.SH","20260720","20260630","000001.SZ",90000.0,500.0,4.11],
	["510300.SH","20260720","","601318.SH",80000.0,200.0,3.60],
	["510300.SH","","","600000.SH",100.0,10.0,1.00]
]}}`)

	rows, err := normalizeETFPortfolio(resp, "510300.SH", SupplyChainSourceTushare, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, rows, 3, "end_date 与 ann_date 都缺的行跳过（PIT 身份不完整）")

	require.Equal(t, "510300.SH", rows[0].ETFCode)
	require.Equal(t, "600519.SH", rows[0].Symbol)
	require.InDelta(t, 5.23, rows[0].Weight, 1e-12)
	require.Equal(t, "2026-06-30", rows[0].ObsDate.UTC().Format("2006-01-02"), "obs_date=end_date（报告期）")

	require.Equal(t, "2026-07-20", rows[2].ObsDate.UTC().Format("2006-01-02"), "end_date 缺失退化为 ann_date")
}

func TestNormalizeETFPortfolio_RejectsMissingSymbol(t *testing.T) {
	resp := decodeTushareBody(t, `{"code":0,"msg":"","data":{"fields":["ts_code","end_date"],"items":[["510300.SH","20260630"]]}}`)
	_, err := normalizeETFPortfolio(resp, "510300.SH", SupplyChainSourceTushare, time.Now().UTC())
	require.Error(t, err)
}

// ── Fetch*：错误传播（破坏验证②的 Red 腿） ──────────────────────────────

// TestFetchSWIndustryClassify_PropagatesSaveError 是破坏验证②的 Red 腿：
// 导入器吞错（log 后 continue、返回 nil）→ 本测试红。既有
// FetchIndexConstituents 正是那个吞错形态（只 Warn 不返回 error）——新代码
// 不许继承这个反模式。
func TestFetchSWIndustryClassify_PropagatesSaveError(t *testing.T) {
	store := &failingSupplyChainStore{err: errors.New("db on fire")}
	client := newSupplyChainTestClient(stubTushare(t, indexClassifyBody, 200), store)

	nodes, err := client.FetchSWIndustryClassify(context.Background())
	require.Error(t, err, "Save 失败必须向上传播（不许 log 后 continue 返回 nil）")
	require.Nil(t, nodes)
	require.Contains(t, err.Error(), "db on fire", "原始错误必须可追溯")
}

func TestFetchSWIndustryMembers_PropagatesSaveError(t *testing.T) {
	store := &failingSupplyChainStore{err: errors.New("db on fire")}
	client := newSupplyChainTestClient(stubTushare(t, indexMemberBody, 200), store)

	edges, err := client.FetchSWIndustryMembers(context.Background(), "801012.SI")
	require.Error(t, err)
	require.Nil(t, edges)
	require.Contains(t, err.Error(), "db on fire")
}

func TestFetchETFPortfolio_PropagatesSaveError(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","ann_date","end_date","symbol","mkv","amount","stk_mkv_ratio"],"items":[["510300.SH","20260720","20260630","600519.SH",1.0,1.0,5.23]]}}`
	store := &failingSupplyChainStore{err: errors.New("db on fire")}
	client := newSupplyChainTestClient(stubTushare(t, body, 200), store)

	rows, err := client.FetchETFPortfolio(context.Background(), "510300.SH", "")
	require.Error(t, err)
	require.Nil(t, rows)
	require.Contains(t, err.Error(), "db on fire")
}

// TestFetchSWIndustryClassify_PropagatesCallError HTTP 层与信封层失败同样传播。
func TestFetchSWIndustryClassify_PropagatesCallError(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"HTTP 500", `upstream exploded`, 500},
		{"tushare 错误信封", `{"code":-1,"msg":"permission denied"}`, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newSupplyChainTestClient(stubTushare(t, tc.body, tc.status), &recordingSupplyChainStore{})
			nodes, err := client.FetchSWIndustryClassify(context.Background())
			require.Error(t, err, "上游失败必须传播（%s）", tc.name)
			require.Nil(t, nodes)
		})
	}
}

// TestFetchSWIndustryClassify_RejectsEmptyIndexCode 参数校验先于 HTTP。
func TestFetchSWIndustryMembers_RejectsEmptyIndexCode(t *testing.T) {
	client := newSupplyChainTestClient("http://127.0.0.1:1", &recordingSupplyChainStore{})
	edges, err := client.FetchSWIndustryMembers(context.Background(), "")
	require.Error(t, err)
	require.Nil(t, edges)
}

// ── Fetch*：happy path 经真 stub 全链路 ─────────────────────────────────

func TestFetchSWIndustryClassify_SavesNormalizedRows(t *testing.T) {
	store := &recordingSupplyChainStore{}
	client := newSupplyChainTestClient(stubTushare(t, indexClassifyBody, 200), store)

	nodes, err := client.FetchSWIndustryClassify(context.Background())
	require.NoError(t, err)
	require.Len(t, nodes, 4)
	require.Len(t, store.tree, 4, "normalize 出来的行必须真的交给 Save")
	require.Equal(t, "801010", store.tree[0].Code)
}

func TestFetchSWIndustryMembers_SavesEdges(t *testing.T) {
	store := &recordingSupplyChainStore{}
	client := newSupplyChainTestClient(stubTushare(t, indexMemberBody, 200), store)

	edges, err := client.FetchSWIndustryMembers(context.Background(), "801012.SI")
	require.NoError(t, err)
	require.Len(t, edges, 2)
	require.Len(t, store.edges, 2)
	require.Equal(t, domain.RelationIndustryMember, store.edges[0].Relation)
}

func TestFetchETFPortfolio_SavesConstituents(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"fields":["ts_code","ann_date","end_date","symbol","mkv","amount","stk_mkv_ratio"],"items":[["510300.SH","20260720","20260630","600519.SH",1.0,1.0,5.23]]}}`
	store := &recordingSupplyChainStore{}
	client := newSupplyChainTestClient(stubTushare(t, body, 200), store)

	rows, err := client.FetchETFPortfolio(context.Background(), "510300.SH", "")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, store.etf, 1)
	require.Equal(t, "510300.SH", store.etf[0].ETFCode)
}

// ── seed CSV 导入 ────────────────────────────────────────────────────────

func TestImportIndustryVitalityCSV_HappyPath(t *testing.T) {
	store := &recordingSupplyChainStore{}
	csv := "industry_code,metric,obs_date,value,unit,source,as_of\n" +
		"801080.SI,gross_margin,2026-06-30,13.42,%,seed-csv,2026-10-12T00:00:00Z\n" +
		"801080.SI,revenue_yoy,2026-06-30,-2.31,%,seed-csv,2026-10-12T00:00:00Z\n" +
		"801010.SI,gross_margin,2026-06-30,21.07,%,seed-csv,2026-10-12T00:00:00Z\n"

	n, err := ImportIndustryVitalityCSV(context.Background(), store, strings.NewReader(csv))
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Len(t, store.vitality, 3)

	require.Equal(t, "801080.SI", store.vitality[0].IndustryCode)
	require.Equal(t, "gross_margin", store.vitality[0].Metric)
	require.Equal(t, "2026-06-30", store.vitality[0].ObsDate.UTC().Format("2006-01-02"))
	require.InDelta(t, 13.42, store.vitality[0].Value, 1e-12)
	require.Equal(t, "seed-csv", store.vitality[0].Source, "source 取自 CSV 列（人工维护标识）")
	require.Equal(t, "2026-10-12", store.vitality[0].AsOf.UTC().Format("2006-01-02"), "as_of 取自 CSV 列")
}

func TestImportIndustryVitalityCSV_AsOfDefaultsToImportTime(t *testing.T) {
	store := &recordingSupplyChainStore{}
	csv := "industry_code,metric,obs_date,value,source\n801080.SI,gross_margin,2026-06-30,13.42,seed-csv\n"

	n, err := ImportIndustryVitalityCSV(context.Background(), store, strings.NewReader(csv))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Len(t, store.vitality, 1)
	require.False(t, store.vitality[0].AsOf.IsZero(), "as_of 缺省取导入时刻，不能是零值")
	require.WithinDuration(t, time.Now(), store.vitality[0].AsOf, time.Minute)
	require.Empty(t, store.vitality[0].Unit, "unit 缺省为空串")
}

func TestImportIndustryVitalityCSV_HeaderOnlyIsNoOp(t *testing.T) {
	store := &recordingSupplyChainStore{}
	n, err := ImportIndustryVitalityCSV(context.Background(), store,
		strings.NewReader("industry_code,metric,obs_date,value,source\n"))
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.Empty(t, store.vitality, "空文件不应触发 Save")
}

func TestImportIndustryVitalityCSV_RejectsBadRows(t *testing.T) {
	cases := []struct {
		name string
		csv  string
		want string
	}{
		{"obs_date 格式错", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026/06/30,1.0,seed\n", "obs_date"},
		{"value 非数值", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,abc,seed\n", "value"},
		// ParseFloat 对 "NaN"/"Inf"/"Infinity" 返回 err=nil（已实测）——CSV 层
		// 必须显式拦，否则文案「不是有限数值」名不副实，且错误归属会漂到
		// storage 层（审查发现②的复现用例）。
		{"value 为 NaN", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,NaN,seed\n", "value"},
		{"value 为 Inf", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,Inf,seed\n", "value"},
		{"value 为 Infinity", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,Infinity,seed\n", "value"},
		{"source 为空", "industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,1.0,\n", "source"},
		{"industry_code 为空", "industry_code,metric,obs_date,value,source\n,gm,2026-06-30,1.0,seed\n", "industry_code"},
		{"as_of 格式错", "industry_code,metric,obs_date,value,source,as_of\n801080.SI,gm,2026-06-30,1.0,seed,not-a-time\n", "as_of"},
		{"缺必填列", "industry_code,metric,obs_date,value\n801080.SI,gm,2026-06-30,1.0\n", "source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingSupplyChainStore{}
			n, err := ImportIndustryVitalityCSV(context.Background(), store, strings.NewReader(tc.csv))
			require.Error(t, err, "坏行必须报错（不跳行不吞错）")
			require.Equal(t, 0, n)
			require.Empty(t, store.vitality, "坏行不许落库")
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestImportIndustryVitalityCSV_RejectsNilStore(t *testing.T) {
	n, err := ImportIndustryVitalityCSV(context.Background(), nil,
		strings.NewReader("industry_code,metric,obs_date,value,source\n801080.SI,gm,2026-06-30,1.0,seed\n"))
	require.Error(t, err)
	require.Equal(t, 0, n)
}

// ── 小工具 ───────────────────────────────────────────────────────────────

// decodeTushareBody 把 stub body 解成 TushareResponse（供 normalize 单测）。
func decodeTushareBody(t *testing.T, body string) *TushareResponse {
	t.Helper()
	resp := &TushareResponse{}
	require.NoError(t, json.Unmarshal([]byte(body), resp), "stub body 应是合法 tushare 信封")
	require.Equal(t, 0, resp.Code)
	return resp
}
