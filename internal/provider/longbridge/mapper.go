package longbridge

import (
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

func price(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(max(0, -d.Exponent()))
	return &s
}

func mapPrePostQuote(v *quote.PrePostQuote) *model.PrePostQuote {
	if v == nil {
		return nil
	}
	return &model.PrePostQuote{
		LastDone:  price(v.LastDone),
		Timestamp: v.Timestamp,
		Volume:    v.Volume,
		Turnover:  price(v.Turnover),
		High:      price(v.High),
		Low:       price(v.Low),
		PrevClose: price(v.PrevClose),
	}
}

func mapSecurityQuote(v *quote.SecurityQuote) *model.SecurityQuote {
	if v == nil {
		return nil
	}
	return &model.SecurityQuote{
		Symbol:          v.Symbol,
		LastDone:        price(v.LastDone),
		PrevClose:       price(v.PrevClose),
		Open:            price(v.Open),
		High:            price(v.High),
		Low:             price(v.Low),
		Timestamp:       v.Timestamp,
		Volume:          v.Volume,
		Turnover:        price(v.Turnover),
		TradeStatus:     int32(v.TradeStatus),
		PreMarketQuote:  mapPrePostQuote(v.PreMarketQuote),
		PostMarketQuote: mapPrePostQuote(v.PostMarketQuote),
		OverNightQuote:  mapPrePostQuote(v.OverNightQuote),
	}
}

func mapStaticInfo(v *quote.StaticInfo) *model.StaticInfo {
	if v == nil {
		return nil
	}
	return &model.StaticInfo{
		Symbol:            v.Symbol,
		NameCn:            v.NameCn,
		NameEn:            v.NameEn,
		NameHk:            v.NameHk,
		Exchange:          v.Exchange,
		Currency:          v.Currency,
		LotSize:           v.LotSize,
		TotalShares:       v.TotalShares,
		CirculatingShares: v.CirculatingShares,
		HkShares:          v.HkShares,
		Eps:               price(v.Eps),
		EpsTtm:            price(v.EpsTtm),
		Bps:               price(v.Bps),
		DividendYield:     v.DividendYield,
		StockDerivatives:  v.StockDerivatives,
	}
}

func mapCandlestick(v *quote.Candlestick) *model.Candlestick {
	if v == nil {
		return nil
	}
	return &model.Candlestick{
		Close:     price(v.Close),
		Open:      price(v.Open),
		Low:       price(v.Low),
		High:      price(v.High),
		Volume:    v.Volume,
		Turnover:  price(v.Turnover),
		Timestamp: v.Timestamp,
	}
}

func mapDepth(v *quote.Depth) *model.Depth {
	if v == nil {
		return nil
	}
	return &model.Depth{
		Position: v.Position,
		Price:    price(v.Price),
		Volume:   v.Volume,
		OrderNum: v.OrderNum,
	}
}

func mapSecurityDepth(v *quote.SecurityDepth) *model.SecurityDepth {
	if v == nil {
		return nil
	}
	return &model.SecurityDepth{
		Symbol: v.Symbol,
		Ask:    mapSlice(v.Ask, mapDepth),
		Bid:    mapSlice(v.Bid, mapDepth),
	}
}

func mapTrade(v *quote.Trade) *model.Trade {
	if v == nil {
		return nil
	}
	return &model.Trade{
		Price:        v.Price,
		Volume:       v.Volume,
		Timestamp:    v.Timestamp,
		TradeType:    v.TradeType,
		Direction:    v.Direction,
		TradeSession: int32(v.TradeSession),
	}
}

func mapIntradayLine(v *quote.IntradayLine) *model.IntradayLine {
	if v == nil {
		return nil
	}
	return &model.IntradayLine{
		Price:     price(v.Price),
		Timestamp: v.Timestamp,
		Volume:    v.Volume,
		Turnover:  price(v.Turnover),
		AvgPrice:  price(v.AvgPrice),
	}
}

func mapTradePeriod(v *quote.TradePeriod) *model.TradePeriod {
	if v == nil {
		return nil
	}
	return &model.TradePeriod{
		BegTime:      v.BegTime,
		EndTime:      v.EndTime,
		TradeSession: int32(v.TradeSession),
	}
}

func mapMarketTradingSession(v *quote.MarketTradingSession) *model.MarketTradingSession {
	if v == nil {
		return nil
	}
	return &model.MarketTradingSession{
		Market:       string(v.Market),
		TradeSession: mapSlice(v.TradeSession, mapTradePeriod),
	}
}

func mapSecurityCalcIndex(v *quote.SecurityCalcIndex) *model.SecurityCalcIndex {
	if v == nil {
		return nil
	}
	return &model.SecurityCalcIndex{
		Symbol:                v.Symbol,
		LastDone:              price(v.LastDone),
		ChangeVal:             price(v.ChangeVal),
		ChangeRate:            price(v.ChangeRate),
		Volume:                v.Volume,
		Turnover:              price(v.Turnover),
		YtdChangeRate:         price(v.YtdChangeRate),
		TurnoverRate:          price(v.TurnoverRate),
		TotalMarketValue:      price(v.TotalMarketValue),
		CapitalFlow:           price(v.CapitalFlow),
		Amplitude:             price(v.Amplitude),
		VolumeRatio:           price(v.VolumeRatio),
		PeTtmRatio:            price(v.PeTtmRatio),
		PbRatio:               price(v.PbRatio),
		DividendRatioTtm:      price(v.DividendRatioTtm),
		FiveDayChangeRate:     price(v.FiveDayChangeRate),
		TenDayChangeRate:      price(v.TenDayChangeRate),
		HalfYearChangeRate:    price(v.HalfYearChangeRate),
		FiveMinutesChangeRate: price(v.FiveMinutesChangeRate),
		ExpiryDate:            v.ExpiryDate,
		StrikePrice:           price(v.StrikePrice),
		UpperStrikePrice:      price(v.UpperStrikePrice),
		LowerStrikePrice:      price(v.LowerStrikePrice),
		OutstandingQty:        price(v.OutstandingQty),
		OutstandingRatio:      price(v.OutstandingRatio),
		Premium:               price(v.Premium),
		ItmOtm:                price(v.ItmOtm),
		ImpliedVolatility:     price(v.ImpliedVolatility),
		WarrantDelta:          price(v.WarrantDelta),
		CallPrice:             price(v.CallPrice),
		ToCallPrice:           price(v.ToCallPrice),
		EffectiveLeverage:     price(v.EffectiveLeverage),
		LeverageRatio:         price(v.LeverageRatio),
		ConversionRatio:       price(v.ConversionRatio),
		BalancePoint:          price(v.BalancePoint),
		OpenInterest:          v.OpenInterest,
		Delta:                 price(v.Delta),
		Gamma:                 price(v.Gamma),
		Theta:                 price(v.Theta),
		Vega:                  price(v.Vega),
		Rho:                   price(v.Rho),
	}
}

func mapSlice[A, B any](in []*A, f func(*A) *B) []*B {
	out := make([]*B, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, f(v))
		}
	}
	return out
}
