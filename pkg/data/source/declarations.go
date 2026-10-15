package source

import "github.com/ruoxizhnya/quant-trading/pkg/domain/market"

// 本文件集中登记所有已接入 connector 的注册声明（时间语义 + 频率口径），
// 遵循项目「声明即登记」约定：新增 connector 必须在此处为其每个
// SupportedTypes 成员补上声明，否则 Registry.Register fail-closed 拒收
// （未声明 = 拒绝注册，见 contract.go）。
//
// 声明口径的裁决依据（保守：不早于真实可见时刻）：
//   - 日更聚合数据（日线 bar / 资金流 / 龙虎榜）→ after_close；
//   - 披露驱动数据（财报 / 公告 / 宏观 / 新闻）→ announcement_day；
//   - 实时快照 / 日内 bar（观察即可见 / 周期结束可见）→ custom + 判定表达。
//
// 待办边界（D1 在 SPEC 已点名，此处如实登记不越界）：
//   - mootdx 分钟表 amount 为 NULL 时 Turnover 记 0 —— 字段级折零由 D2 收编，
//     不改 ETL 行为，仅在契约文档登记；
//   - mootdx 的 volume / amount **单位未实测**（TDX 惯例是「手」，项目内无证据），
//     故其 Schema 把 Unit 如实留空、同步边界也不放换算点 —— 没有实测就不声明。
//     DATA-05 核对（2026-10-15）：该源今天以 nil transport 注册，整条日线 fallback
//     链不可达；接线时先实测单位再声明，守卫见 cmd/data/ohlc_units_guard_test.go；
//   - 产业链 fetcher 不走 Registry（不建 adapter），其时间语义在契约文档中
//     成文登记为已知边界（建 adapter 是 D5 的事）。

// Declarations implements DataSourceAdapter.Declarations.
// Tushare 是 A 股日线（前复权，唯一进回测口径）与财报字段的来源。
func (a *TushareAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeOHLCDaily,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
		{
			// 财报：披露日对齐防前视。可见时间锚点是**披露日**（规范层
			// market.Fundamental.AnnDate，SPEC「披露日即可见时间点」），
			// 不是 DataItem.TradeTime —— 财报连接器的 TradeTime 承载的是
			// **报告期截止日**（pkg/data/tushare.go normalizeFundamentals
			// 取 end_date），用它当锚点等于把前视洞放回来。
			DataType:      DataTypeFundamental,
			TimeSemantics: market.VisibleOnAnnouncementDay,
			VisibleField:  VisibleFieldAnnDate,
			Frequency:     market.FreqSpecEvent,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
// 宏观指标为发布驱动的事件流（发布后方可见）。wire schema 只有 obs_date
// （**观测日**，月频取当月首日）与 as_of（导入时刻），上游不携带发布日
// 字段；锚点取 obs_date，对齐 P2-1 既有「obs_date 为 PIT」的口径。
// 注意：观测日**早于**真实发布日（9 月 CPI 10 月中才发布），此处声明与
// 真实可见时刻的缺口已登记为跟进项（TASKS D2-R1）——是否改判宏观层 PIT 门
// 需 P2-1 层裁决，不在 D2 范围。
func (a *TushareMacroAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeMacro,
			TimeSemantics: market.VisibleOnAnnouncementDay,
			VisibleField:  "obs_date",
			Frequency:     market.FreqSpecEvent,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *MootdxAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			// 实时行情是观察即可见的快照，不参与回测，但口径必须如实声明。
			DataType:      DataTypeRealtime,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = fetch_time（实时快照，观察时刻即可见）",
			VisibleField:  VisibleFieldTradeTime, // DataItem.TradeTime = 快照时刻
			Frequency:     market.FreqSpecIntraday,
		},
		{
			// 分钟 bar 的可见时间点 = bar 周期结束（蓝图 §4.3，D1 IntradayVisibleAt）。
			DataType:      DataTypeOHLCMinute,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = bar_period_end（分钟 bar 周期结束即可见）",
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecIntraday,
		},
		{
			DataType:      DataTypeOHLCDaily,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *EastmoneyAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeCapitalFlow,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *EastmoneySectorsAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			// 板块行情是盘中实时快照（push2），观察时刻即可见。
			DataType:      DataTypeSectors,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = fetch_time（盘中板块快照，观察时刻即可见）",
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecIntraday,
		},
		{
			// 个股板块映射为参考数据，按观察时刻可见。
			DataType:      DataTypeStockSector,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = fetch_time（个股板块映射，观察时刻即可见）",
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecEvent,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *EastmoneyTopListAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeTopList,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
		{
			DataType:      DataTypeLimitUpPool,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *JuchaoAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			// 公告：公告时间（ann_time）即可见，披露驱动事件流。
			DataType:      DataTypeAnnounce,
			TimeSemantics: market.VisibleOnAnnouncementDay,
			VisibleField:  "ann_time",
			Frequency:     market.FreqSpecEvent,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *XueqiuAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeHotSearch,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = fetch_time（热搜榜快照，观察时刻即可见）",
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecIntraday,
		},
		{
			// 新闻：发布时间（publish_time）即可见。
			DataType:      DataTypeNews,
			TimeSemantics: market.VisibleCustom,
			VisibleExpr:   "visible_at = publish_time（新闻发布时刻即可见）",
			VisibleField:  "publish_time",
			Frequency:     market.FreqSpecEvent,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *AlphaVantageAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeGlobalOHLCV,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
	}
}

// Declarations implements DataSourceAdapter.Declarations.
func (a *YahooFinanceAdapter) Declarations() []ConnectorDeclaration {
	return []ConnectorDeclaration{
		{
			DataType:      DataTypeGlobalOHLCV,
			TimeSemantics: market.VisibleAfterClose,
			VisibleField:  VisibleFieldTradeTime,
			Frequency:     market.FreqSpecDaily,
		},
	}
}
