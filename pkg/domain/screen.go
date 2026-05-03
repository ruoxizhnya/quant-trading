package domain

type ScreenFilters struct {
	PE_min           *float64 `json:"pe_min"`
	PE_max           *float64 `json:"pe_max"`
	PB_min           *float64 `json:"pb_min"`
	PB_max           *float64 `json:"pb_max"`
	PS_min           *float64 `json:"ps_min"`
	PS_max           *float64 `json:"ps_max"`
	ROE_min          *float64 `json:"roe_min"`
	ROA_min          *float64 `json:"roa_min"`
	DebtToEquity_max *float64 `json:"debt_to_equity_max"`
	GrossMargin_min  *float64 `json:"gross_margin_min"`
	NetMargin_min    *float64 `json:"net_margin_min"`
	MarketCap_min    *float64 `json:"market_cap_min"`
}

type ScreenRequest struct {
	Filters ScreenFilters `json:"filters"`
	Date    string        `json:"date"`
	Limit   int           `json:"limit"`
}

type ScreenResult struct {
	TsCode       string   `json:"ts_code"`
	PE           *float64 `json:"pe"`
	PB           *float64 `json:"pb"`
	PS           *float64 `json:"ps"`
	ROE          *float64 `json:"roe"`
	ROA          *float64 `json:"roa"`
	DebtToEquity *float64 `json:"debt_to_equity"`
	GrossMargin  *float64 `json:"gross_margin"`
	NetMargin    *float64 `json:"net_margin"`
	MarketCap    *float64 `json:"market_cap"`
}
