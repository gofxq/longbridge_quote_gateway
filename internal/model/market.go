package model

type PrePostQuote struct {
	LastDone  *string `json:"last_done"`
	Timestamp int64   `json:"timestamp"`
	Volume    int64   `json:"volume"`
	Turnover  *string `json:"turnover"`
	High      *string `json:"high"`
	Low       *string `json:"low"`
	PrevClose *string `json:"prev_close"`
}

type SecurityQuote struct {
	Symbol          string        `json:"symbol"`
	LastDone        *string       `json:"last_done"`
	PrevClose       *string       `json:"prev_close"`
	Open            *string       `json:"open"`
	High            *string       `json:"high"`
	Low             *string       `json:"low"`
	Timestamp       int64         `json:"timestamp"`
	Volume          int64         `json:"volume"`
	Turnover        *string       `json:"turnover"`
	TradeStatus     int32         `json:"trade_status"`
	PreMarketQuote  *PrePostQuote `json:"pre_market_quote"`
	PostMarketQuote *PrePostQuote `json:"post_market_quote"`
	OverNightQuote  *PrePostQuote `json:"overnight_quote"`
	Change          *string       `json:"change"`
	ChangePercent   *string       `json:"change_percent"`
}

type StaticInfo struct {
	Symbol            string  `json:"symbol"`
	NameCn            string  `json:"name_cn"`
	NameEn            string  `json:"name_en"`
	NameHk            string  `json:"name_hk"`
	Exchange          string  `json:"exchange"`
	Currency          string  `json:"currency"`
	LotSize           int32   `json:"lot_size"`
	TotalShares       int64   `json:"total_shares"`
	CirculatingShares int64   `json:"circulating_shares"`
	HkShares          int64   `json:"hk_shares"`
	Eps               *string `json:"eps"`
	EpsTtm            *string `json:"eps_ttm"`
	Bps               *string `json:"bps"`
	DividendYield     string  `json:"dividend_yield"`
	StockDerivatives  []int32 `json:"stock_derivatives"`
}

type Candlestick struct {
	Close     *string `json:"close"`
	Open      *string `json:"open"`
	Low       *string `json:"low"`
	High      *string `json:"high"`
	Volume    int64   `json:"volume"`
	Turnover  *string `json:"turnover"`
	Timestamp int64   `json:"timestamp"`
}

type Depth struct {
	Position int32   `json:"position"`
	Price    *string `json:"price"`
	Volume   int64   `json:"volume"`
	OrderNum int64   `json:"order_num"`
}

type SecurityDepth struct {
	Symbol string   `json:"symbol"`
	Ask    []*Depth `json:"ask"`
	Bid    []*Depth `json:"bid"`
}

type Trade struct {
	Price        string `json:"price"`
	Volume       int64  `json:"volume"`
	Timestamp    int64  `json:"timestamp"`
	TradeType    string `json:"trade_type"`
	Direction    int32  `json:"direction"`
	TradeSession int32  `json:"trade_session"`
}

type IntradayLine struct {
	Price     *string `json:"price"`
	Timestamp int64   `json:"timestamp"`
	Volume    int64   `json:"volume"`
	Turnover  *string `json:"turnover"`
	AvgPrice  *string `json:"avg_price"`
}

type TradePeriod struct {
	BegTime      int32 `json:"beg_time"`
	EndTime      int32 `json:"end_time"`
	TradeSession int32 `json:"trade_session"`
}

type MarketTradingSession struct {
	Market       string         `json:"market"`
	TradeSession []*TradePeriod `json:"trade_session"`
}

type SecurityCalcIndex struct {
	Symbol                string  `json:"symbol"`
	LastDone              *string `json:"last_done"`
	ChangeVal             *string `json:"change_val"`
	ChangeRate            *string `json:"change_rate"`
	Volume                int64   `json:"volume"`
	Turnover              *string `json:"turnover"`
	YtdChangeRate         *string `json:"ytd_change_rate"`
	TurnoverRate          *string `json:"turnover_rate"`
	TotalMarketValue      *string `json:"total_market_value"`
	CapitalFlow           *string `json:"capital_flow"`
	Amplitude             *string `json:"amplitude"`
	VolumeRatio           *string `json:"volume_ratio"`
	PeTtmRatio            *string `json:"pe_ttm_ratio"`
	PbRatio               *string `json:"pb_ratio"`
	DividendRatioTtm      *string `json:"dividend_ratio_ttm"`
	FiveDayChangeRate     *string `json:"five_day_change_rate"`
	TenDayChangeRate      *string `json:"ten_day_change_rate"`
	HalfYearChangeRate    *string `json:"half_year_change_rate"`
	FiveMinutesChangeRate *string `json:"five_minutes_change_rate"`
	ExpiryDate            string  `json:"expiry_date"`
	StrikePrice           *string `json:"strike_price"`
	UpperStrikePrice      *string `json:"upper_strike_price"`
	LowerStrikePrice      *string `json:"lower_strike_price"`
	OutstandingQty        *string `json:"outstanding_qty"`
	OutstandingRatio      *string `json:"outstanding_ratio"`
	Premium               *string `json:"premium"`
	ItmOtm                *string `json:"itm_otm"`
	ImpliedVolatility     *string `json:"implied_volatility"`
	WarrantDelta          *string `json:"warrant_delta"`
	CallPrice             *string `json:"call_price"`
	ToCallPrice           *string `json:"to_call_price"`
	EffectiveLeverage     *string `json:"effective_leverage"`
	LeverageRatio         *string `json:"leverage_ratio"`
	ConversionRatio       *string `json:"conversion_ratio"`
	BalancePoint          *string `json:"balance_point"`
	OpenInterest          int64   `json:"open_interest"`
	Delta                 *string `json:"delta"`
	Gamma                 *string `json:"gamma"`
	Theta                 *string `json:"theta"`
	Vega                  *string `json:"vega"`
	Rho                   *string `json:"rho"`
}
