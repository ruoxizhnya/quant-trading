package data

// 产业链静态数据层导入面（P2-2，TASKS.md §五 遗留待裁决）。
//
// 三个 tushare 导入路径 + 一个 seed CSV 导入路径，全部落 quant schema 四张表
// （pkg/storage.SupplyChainSchemaDDL / migrate() Migration 049）：
//
//	index_classify → quant.sw_industry_tree    申万行业树（is_leaf 由父子关系推导）
//	index_member   → quant.supply_chain_edge   行业成分（relation=industry_member，
//	                                               upstream=成分股 symbol，downstream=行业 code）
//	fund_portfolio → quant.etf_constituent     ETF 持仓（obs_date=报告期，PIT）
//	seed CSV       → quant.industry_vitality   行业景气指标（无现成 API，人工维护）
//
// 纪律（与既有 Fetch* 的区别，见 file 头注释）：
//   - **错误一律向上传播**：call 失败、响应形状不符契约、Save 失败，全部
//     apperrors 包装后 return nil, err。绝不 log 后 continue——那会让「上游
//     挂了」伪装成「导入了 0 行」（破坏验证②的 Red 腿）。
//   - 幂等在存储层：ON CONFLICT DO UPDATE（见 pkg/storage/supply_chain.go），
//     同批重导不重复插行。
//   - 溯源列：tushare 导入的 source 恒为 "tushare"，as_of 取导入时刻；
//     CSV 导入的 source 取自 CSV 列（人工维护标识），as_of 取 CSV 列或导入时刻。

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// SupplyChainSourceTushare 是产业链四表 source 列的 tushare 取值（CSV 导入
// 用 CSV 自带 source 列，如 "seed-csv"）。
const SupplyChainSourceTushare = "tushare"

// SupplyChainStore 是产业链静态数据层的写入面（pkg/storage.PostgresStore
// 实现）。TushareStore 内嵌它，导入路径的 Save 契约在编译期钉住。
type SupplyChainStore interface {
	SaveIndustryTreeBatch(ctx context.Context, rows []*domain.SWIndustryNode) error
	SaveSupplyChainEdgeBatch(ctx context.Context, rows []*domain.SupplyChainEdge) error
	SaveIndustryVitalityBatch(ctx context.Context, rows []*domain.IndustryVitality) error
	SaveETFConstituentBatch(ctx context.Context, rows []*domain.ETFConstituent) error
}

// ── tushare 导入 ──────────────────────────────────────────────────────────

// FetchSWIndustryClassify 拉取申万行业分类树（index_classify）落库
// quant.sw_industry_tree。返回入库的节点（is_leaf 已推导）。
//
// 不传 index_code/level 参数：拉全量树（一级+二级+三级），叶子标记与父子
// 关系只有在全量在手时才能推导。
func (c *TushareClient) FetchSWIndustryClassify(ctx context.Context) ([]domain.SWIndustryNode, error) {
	resp, err := c.call(ctx, "index_classify", nil,
		"index_code,industry_name,level,industry_code,parent_code,is_pub")
	if err != nil {
		return nil, err
	}

	nodes, err := normalizeSWIndustryClassify(resp, SupplyChainSourceTushare, time.Now().UTC())
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
			"index_classify 响应形状不符合契约（缺 industry_code/industry_name 列）",
			"data.FetchSWIndustryClassify")
	}
	if len(nodes) == 0 {
		return nil, nil
	}

	if err := c.store.SaveIndustryTreeBatch(ctx, nodePtrs(nodes)); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"申万行业树落库失败", "data.FetchSWIndustryClassify")
	}
	c.logger.Info().Int("count", len(nodes)).Msg("SW industry tree fetched and saved")
	return nodes, nil
}

// FetchSWIndustryMembers 拉取一个申万行业的成分股（index_member）落库为
// quant.supply_chain_edge（relation=industry_member）。
//
// **口径（P2-2-R2 裁决①）：当前成分快照，非 PIT。** 仅 out_date 为空的段
// （当前在表成员）入边表，历史出表段在 normalize 层过滤——理由与处理细节
// 见 normalizeSWIndustryMembers 注释。
//
// 成分关系入边表而非另建表：边表 upstream/downstream 的设计语义就是
// 「symbol 或行业 code 的映射」（见 domain.SupplyChainEdge 注释），行业成分
// 是同一形态的映射；唯一键 (upstream, downstream, relation) 天然隔离不同
// 关系。weight 恒 0——index_member 不含权重字段。
func (c *TushareClient) FetchSWIndustryMembers(ctx context.Context, indexCode string) ([]domain.SupplyChainEdge, error) {
	if indexCode == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"indexCode 不可为空")
	}

	resp, err := c.call(ctx, "index_member",
		map[string]interface{}{"index_code": indexCode},
		"index_code,con_code,in_date,out_date")
	if err != nil {
		return nil, err
	}

	edges, err := normalizeSWIndustryMembers(resp, indexCode, SupplyChainSourceTushare, time.Now().UTC())
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
			"index_member 响应形状不符合契约", "data.FetchSWIndustryMembers")
	}
	if len(edges) == 0 {
		return nil, nil
	}

	if err := c.store.SaveSupplyChainEdgeBatch(ctx, edgePtrs(edges)); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"申万行业成分落库失败", "data.FetchSWIndustryMembers")
	}
	c.logger.Info().Str("index", indexCode).Int("count", len(edges)).
		Msg("SW industry members fetched and saved as supply chain edges")
	return edges, nil
}

// FetchETFPortfolio 拉取 ETF 持仓（fund_portfolio）落库 quant.etf_constituent。
// period 为报告期（YYYYMMDD），空串时由 tushare 返回默认期。
//
// obs_date 取响应的 end_date（报告期），缺失时退化为 ann_date（公告日）——
// PIT 口径上报告期才是「这批成分生效的时点」。weight 取 stk_mkv_ratio
// （占净值比例，百分数）。
func (c *TushareClient) FetchETFPortfolio(ctx context.Context, etfCode, period string) ([]domain.ETFConstituent, error) {
	if etfCode == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"etfCode 不可为空")
	}
	params := map[string]interface{}{"ts_code": etfCode}
	if period != "" {
		params["end_date"] = period
	}

	resp, err := c.call(ctx, "fund_portfolio", params,
		"ts_code,ann_date,end_date,symbol,mkv,amount,stk_mkv_ratio")
	if err != nil {
		return nil, err
	}

	rows, err := normalizeETFPortfolio(resp, etfCode, SupplyChainSourceTushare, time.Now().UTC())
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
			"fund_portfolio 响应形状不符合契约（缺 symbol 列）", "data.FetchETFPortfolio")
	}
	if len(rows) == 0 {
		return nil, nil
	}

	if err := c.store.SaveETFConstituentBatch(ctx, etfConstituentPtrs(rows)); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"ETF 成分落库失败", "data.FetchETFPortfolio")
	}
	c.logger.Info().Str("etf", etfCode).Str("period", period).Int("count", len(rows)).
		Msg("ETF portfolio fetched and saved")
	return rows, nil
}

// ── normalize（纯函数：TushareResponse → domain 行） ─────────────────────

// tushareColumnIndex 在响应的 Fields 头里按**名字优先级**找列：names 靠前的
// 优先（如 industry_code 优先于 index_code），与列在响应里的实际顺序无关。
// 全部找不到返回 -1。按名定位而不是固定下标：tushare 各 API 的字段顺序不保证
// 稳定，按名定位让 normalize 对字段重排保持宽容；缺关键列则由调用方报
// 「形状不符契约」（fail-loud，不静默跳行）。
func tushareColumnIndex(resp *TushareResponse, names ...string) int {
	for _, n := range names {
		for i, f := range resp.Data.Fields {
			if f == n {
				return i
			}
		}
	}
	return -1
}

// tushareCell 取 item 在列 idx 的值（越界/nil → 零值），转字符串。
func tushareCell(item []any, idx int) string {
	if idx < 0 || idx >= len(item) || item[idx] == nil {
		return ""
	}
	return fieldToStr(item[idx])
}

// tushareCellFloat 取 item 在列 idx 的值，转 float64。
func tushareCellFloat(item []any, idx int) float64 {
	if idx < 0 || idx >= len(item) || item[idx] == nil {
		return 0
	}
	return fieldToFloat(item[idx])
}

// parseSWLevel 把申万层级字段归一为整数：接受 "L1"/"l1"/"1"。
// 无法解析时返回 (0, false)——调用方按形状不符契约处理。
func parseSWLevel(s string) (int, bool) {
	t := strings.TrimSpace(strings.ToUpper(s))
	t = strings.TrimPrefix(t, "L")
	if t == "" {
		return 0, false
	}
	n, err := strconv.Atoi(t)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// normalizeSWIndustryClassify 把 index_classify 响应转为行业树节点。
// code 优先取 industry_code（申万行业代码），退化取 index_code；level 解析
// 失败**整批报错**（形状不符契约——静默默认 1 会把三级行业错标成一级，
// 比丢一行更危险）。缺关键列（industry_code/industry_name）同样报错。
func normalizeSWIndustryClassify(resp *TushareResponse, source string, asOf time.Time) ([]domain.SWIndustryNode, error) {
	iCode := tushareColumnIndex(resp, "industry_code", "index_code")
	iName := tushareColumnIndex(resp, "industry_name", "name")
	iLevel := tushareColumnIndex(resp, "level")
	iParent := tushareColumnIndex(resp, "parent_code")
	if iCode < 0 || iName < 0 {
		return nil, fmt.Errorf("index_classify 响应缺 industry_code/industry_name 列（实际 fields=%v）", resp.Data.Fields)
	}

	var nodes []domain.SWIndustryNode
	for _, item := range resp.Data.Items {
		code := strings.TrimSpace(tushareCell(item, iCode))
		name := strings.TrimSpace(tushareCell(item, iName))
		if code == "" || name == "" {
			continue
		}
		level := 1
		if iLevel >= 0 {
			n, ok := parseSWLevel(tushareCell(item, iLevel))
			if !ok {
				return nil, fmt.Errorf("index_classify 行 level=%q 无法解析（code=%s）",
					tushareCell(item, iLevel), code)
			}
			level = n
		}
		nodes = append(nodes, domain.SWIndustryNode{
			Code:       code,
			ParentCode: strings.TrimSpace(tushareCell(item, iParent)),
			Level:      level,
			Name:       name,
			Source:     source,
			AsOf:       asOf,
		})
	}
	markSWIndustryLeaves(nodes)
	return nodes, nil
}

// markSWIndustryLeaves 按父子关系推导 is_leaf：没有任何节点以本节点为父，
// 即为叶子。行业树的叶子标记不来自上游 API（index_classify 无此字段），由
// 数据本身决定——重导后自动修正。
func markSWIndustryLeaves(nodes []domain.SWIndustryNode) {
	parents := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if n.ParentCode != "" {
			parents[n.ParentCode] = true
		}
	}
	for i := range nodes {
		nodes[i].IsLeaf = !parents[nodes[i].Code]
	}
}

// normalizeSWIndustryMembers 把 index_member 响应转为成分边
// （relation=industry_member，upstream=con_code，downstream=index_code）。
// downstream 优先取响应里的 index_code 列，退化用请求参数 indexCode。
//
// **口径（P2-2-R2 裁决①）：仅保留 out_date 为空的段（当前在表成员）；
// out_date 非空的历史出表段不入边表。** 边表唯一键 (upstream,downstream,
// relation) 按成分股折叠多期记录；若历史段一并导入，已出表成员与当前成员
// 同形态落库，K60 消费方按当前成分使用会引入陈旧成员（P2-2-R1 审查实测
// 801010.SI 160 个唯一成分中 56 个已全部出表）。故本 relation 的语义定为
// **「当前成分快照，非 PIT」**——需要历史成分为 PIT 序列时另案表达
// （带有效期的表或新 relation），不在本边表折叠形态里表达。
//
// out_date 列缺失 = fail-loud：请求固定含 out_date（FetchSWIndustryMembers），
// 缺列意味着当前/历史无法判定，静默按空处理会悄然回退到「历史混入」的
// 旧行为——拒绝（与缺 con_code 列同处理）。
func normalizeSWIndustryMembers(resp *TushareResponse, indexCode, source string, asOf time.Time) ([]domain.SupplyChainEdge, error) {
	iIndex := tushareColumnIndex(resp, "index_code")
	iCon := tushareColumnIndex(resp, "con_code", "ts_code")
	iOut := tushareColumnIndex(resp, "out_date")
	if iCon < 0 {
		return nil, fmt.Errorf("index_member 响应缺 con_code 列（实际 fields=%v）", resp.Data.Fields)
	}
	if iOut < 0 {
		return nil, fmt.Errorf("index_member 响应缺 out_date 列（实际 fields=%v）——无法判定当前/历史成分，拒绝静默全保留", resp.Data.Fields)
	}

	var edges []domain.SupplyChainEdge
	for _, item := range resp.Data.Items {
		con := strings.TrimSpace(tushareCell(item, iCon))
		if con == "" {
			continue
		}
		// P2-2-R2 裁决①：仅当前在表段入边表——out_date 非空 = 已出表
		// 历史段，过滤。唯一键按成分股折叠，历史段混入会让 K60 消费方
		// 按当前成分拿到陈旧成员（P2-2-R1 审查实测 56/160 已出表）。
		if strings.TrimSpace(tushareCell(item, iOut)) != "" {
			continue
		}
		down := strings.TrimSpace(tushareCell(item, iIndex))
		if down == "" {
			down = indexCode
		}
		edges = append(edges, domain.SupplyChainEdge{
			Upstream:   con,
			Downstream: down,
			Relation:   domain.RelationIndustryMember,
			Source:     source,
			AsOf:       asOf,
		})
	}
	return edges, nil
}

// normalizeETFPortfolio 把 fund_portfolio 响应转为 ETF 成分行。
// obs_date 取 end_date（报告期），缺失退化为 ann_date；weight 取
// stk_mkv_ratio。缺 obs_date 或 symbol 的行跳过（PIT 身份不完整）。
func normalizeETFPortfolio(resp *TushareResponse, etfCode, source string, asOf time.Time) ([]domain.ETFConstituent, error) {
	iTsCode := tushareColumnIndex(resp, "ts_code")
	iSymbol := tushareColumnIndex(resp, "symbol", "con_code")
	iEnd := tushareColumnIndex(resp, "end_date")
	iAnn := tushareColumnIndex(resp, "ann_date")
	iRatio := tushareColumnIndex(resp, "stk_mkv_ratio", "weight")
	if iSymbol < 0 {
		return nil, fmt.Errorf("fund_portfolio 响应缺 symbol 列（实际 fields=%v）", resp.Data.Fields)
	}

	var rows []domain.ETFConstituent
	for _, item := range resp.Data.Items {
		symbol := strings.TrimSpace(tushareCell(item, iSymbol))
		if symbol == "" {
			continue
		}
		obs, ok := parseTushareDate(tushareCell(item, iEnd))
		if !ok {
			obs, ok = parseTushareDate(tushareCell(item, iAnn))
		}
		if !ok {
			continue // PIT 身份不完整的行不进库
		}
		code := strings.TrimSpace(tushareCell(item, iTsCode))
		if code == "" {
			code = etfCode
		}
		rows = append(rows, domain.ETFConstituent{
			ETFCode: code,
			Symbol:  symbol,
			Weight:  tushareCellFloat(item, iRatio),
			ObsDate: obs,
			Source:  source,
			AsOf:    asOf,
		})
	}
	return rows, nil
}

// parseTushareDate 解析 tushare 的 YYYYMMDD 日期串；空串/解析失败 → (零值, false)。
func parseTushareDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// nodePtrs / edgePtrs / etfConstituentPtrs —— 批量 Save 接口收指针切片，
// 与 tushare.go 既有 FetchIndexConstituents 的 ptrs 转换同款。
func nodePtrs(nodes []domain.SWIndustryNode) []*domain.SWIndustryNode {
	ptrs := make([]*domain.SWIndustryNode, len(nodes))
	for i := range nodes {
		ptrs[i] = &nodes[i]
	}
	return ptrs
}

func edgePtrs(edges []domain.SupplyChainEdge) []*domain.SupplyChainEdge {
	ptrs := make([]*domain.SupplyChainEdge, len(edges))
	for i := range edges {
		ptrs[i] = &edges[i]
	}
	return ptrs
}

func etfConstituentPtrs(rows []domain.ETFConstituent) []*domain.ETFConstituent {
	ptrs := make([]*domain.ETFConstituent, len(rows))
	for i := range rows {
		ptrs[i] = &rows[i]
	}
	return ptrs
}

// ── seed CSV 导入（行业景气指标，首版无现成 API） ────────────────────────

// vitalitySeedColumns 是 seed CSV 的必填列；as_of / unit 可选。
var vitalitySeedRequired = []string{"industry_code", "metric", "obs_date", "value", "source"}

// ImportIndustryVitalityCSV 从 seed CSV 导入行业景气指标。
//
// CSV 表头（必含，列顺序不限）：
//
//		industry_code,metric,obs_date,value,unit,source[,as_of]
//
//	  - obs_date 必须为 YYYY-MM-DD；as_of 为 RFC3339，缺省取导入时刻；
//	  - unit 可空（入库为 ""）；
//	  - source 取自 CSV 列（人工维护标识，如 "seed-csv"）——与 tushare 导入
//	    同走 source/as_of 溯源列，唯一键含 source，不同来源各行其行。
//
// 任一行不合法 → apperrors 包装向上传播（带行号），**不跳行不吞错**：半份
// 导入比整份失败更难查（与 Save 批量的事务口径一致——存储层一个事务，
// 要么全落要么全不落）。
func ImportIndustryVitalityCSV(ctx context.Context, store SupplyChainStore, r io.Reader) (int, error) {
	if store == nil {
		return 0, apperrors.New(apperrors.ErrCodeInvalidInput,
			"store 不可为 nil")
	}

	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // 列数由表头定，容忍行内多余字段在取值时发现

	header, err := reader.Read()
	if err != nil {
		return 0, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
			"seed CSV 读取表头失败", "data.ImportIndustryVitalityCSV")
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		// UTF-8 BOM（Windows 编辑器另存的普遍场景）粘在首列名上，U+FEFF 不是
		// TrimSpace 可去字符——先剥 BOM 再 TrimSpace，否则真文件首列名带
		// U+FEFF 匹配不到 industry_code，直接报「缺列」（P2-2-R1）。
		h = strings.TrimPrefix(h, "\ufeff")
		col[strings.TrimSpace(h)] = i
	}
	for _, req := range vitalitySeedRequired {
		if _, ok := col[req]; !ok {
			return 0, apperrors.Wrap(fmt.Errorf("缺列 %q（实际表头=%v）", req, header),
				apperrors.ErrCodeDataQuality, "seed CSV 表头不符合契约", "data.ImportIndustryVitalityCSV")
		}
	}

	importedAt := time.Now().UTC()
	var rows []*domain.IndustryVitality
	lineNo := 1 // 表头占第 1 行
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		lineNo++
		if err != nil {
			return 0, apperrors.Wrap(err, apperrors.ErrCodeDataQuality,
				fmt.Sprintf("seed CSV 第 %d 行解析失败", lineNo), "data.ImportIndustryVitalityCSV")
		}

		row, err := vitalityRowFromCSV(rec, col, lineNo, importedAt)
		if err != nil {
			return 0, err
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return 0, nil
	}
	if err := store.SaveIndustryVitalityBatch(ctx, rows); err != nil {
		return 0, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"seed CSV 景气指标落库失败", "data.ImportIndustryVitalityCSV")
	}
	return len(rows), nil
}

// vitalityRowFromCSV 把一行 CSV 记录转为领域行；不合法即报错（带行号）。
func vitalityRowFromCSV(rec []string, col map[string]int, lineNo int, importedAt time.Time) (*domain.IndustryVitality, error) {
	cell := func(name string) (string, bool) {
		idx, ok := col[name]
		if !ok || idx >= len(rec) {
			return "", !ok // 列存在但值越界按空值处理（FieldsPerRecord=-1 下行可能短）
		}
		return strings.TrimSpace(rec[idx]), true
	}

	row := &domain.IndustryVitality{AsOf: importedAt}
	for _, req := range vitalitySeedRequired {
		v, _ := cell(req)
		if v == "" {
			return nil, apperrors.Wrap(fmt.Errorf("第 %d 行 %s 为空", lineNo, req),
				apperrors.ErrCodeDataQuality, "seed CSV 必填列不可为空", "data.ImportIndustryVitalityCSV")
		}
		switch req {
		case "industry_code":
			row.IndustryCode = v
		case "metric":
			row.Metric = v
		case "source":
			row.Source = v
		case "value":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, apperrors.Wrap(fmt.Errorf("第 %d 行 value=%q: %v", lineNo, v, err),
					apperrors.ErrCodeDataQuality, "seed CSV value 不是有限数值", "data.ImportIndustryVitalityCSV")
			}
			// ParseFloat 接受 "NaN"/"Inf"/"Infinity"（err=nil）——CSV 层显式拦
			// 非有限值，让文案即真相。否则 NaN 会穿过本层到 storage 才被拦，
			// 错误归属误导排障（审查发现：P2-2 自审问题②）。与 storage 的
			// IsNaN/IsInf 校验同口径——纵深防御，两边都要有。
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, apperrors.Wrap(fmt.Errorf("第 %d 行 value=%q（非有限值）", lineNo, v),
					apperrors.ErrCodeDataQuality, "seed CSV value 不是有限数值", "data.ImportIndustryVitalityCSV")
			}
			row.Value = f
		case "obs_date":
			t, err := time.Parse("2006-01-02", v)
			if err != nil {
				return nil, apperrors.Wrap(fmt.Errorf("第 %d 行 obs_date=%q（要求 YYYY-MM-DD）", lineNo, v),
					apperrors.ErrCodeDataQuality, "seed CSV obs_date 格式错误", "data.ImportIndustryVitalityCSV")
			}
			row.ObsDate = t
		}
	}

	if v, ok := cell("unit"); ok {
		row.Unit = v
	}
	if v, ok := cell("as_of"); ok && v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, apperrors.Wrap(fmt.Errorf("第 %d 行 as_of=%q（要求 RFC3339）", lineNo, v),
				apperrors.ErrCodeDataQuality, "seed CSV as_of 格式错误", "data.ImportIndustryVitalityCSV")
		}
		row.AsOf = t
	}
	return row, nil
}
