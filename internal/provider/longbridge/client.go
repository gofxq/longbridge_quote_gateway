package longbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	openapi "github.com/longbridge/openapi-go"
	sdkconfig "github.com/longbridge/openapi-go/config"
	"github.com/longbridge/openapi-go/oauth"
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

type Client struct {
	mu     sync.RWMutex
	sdk    *quote.QuoteContext
	ready  atomic.Bool
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New keeps HTTP available while initial connection attempts back off. The SDK owns reconnection after initialization.
func New(ctx context.Context) (*Client, error) {
	for _, pair := range [][2]string{{"LONGBRIDGE_REGION", "LONGPORT_REGION"}, {"LONGBRIDGE_LANGUAGE", "LONGPORT_LANGUAGE"}} {
		if v := os.Getenv(pair[0]); v != "" {
			if err := os.Setenv(pair[1], v); err != nil {
				return nil, err
			}
		}
	}
	opts := []sdkconfig.Option{}
	if id := os.Getenv("LONGBRIDGE_OAUTH_CLIENT_ID"); id != "" {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(id) {
			return nil, errors.New("invalid OAuth client ID")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.New("OAuth home directory unavailable")
		}
		if _, err := os.Stat(filepath.Join(home, ".longbridge/openapi/tokens", id)); err != nil {
			return nil, errors.New("OAuth token file missing; run cmd/auth first")
		}
		o := oauth.New(id).OnOpenURL(func(string) {})
		authCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := o.Build(authCtx); err != nil {
			return nil, errors.New("OAuth initialization failed; run cmd/auth first")
		}
		opts = append(opts, sdkconfig.WithOAuthClient(o))
	}
	cfg, err := sdkconfig.New(opts...)
	if err != nil {
		return nil, errors.New("invalid Longbridge credentials or configuration")
	}
	cfg.QuoteTransport = "ws"
	cfg.HTTPTimeout = 8 * time.Second
	cfg.AuthTimeout = 8 * time.Second
	cfg.Timeout = 8 * time.Second
	cfg.LogLevel = "error"
	cfg.SetLogger(sdkLogger{})
	ctx, cancel := context.WithCancel(ctx)
	c := &Client{cancel: cancel}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		delay := time.Second
		for {
			if ctx.Err() != nil {
				return
			}
			sdk, err := newQuoteContext(cfg, func(ok bool) { c.ready.Store(ok) })
			if err == nil {
				c.mu.Lock()
				c.sdk = sdk
				c.ready.Store(true)
				c.mu.Unlock()
				return
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			delay = min(delay*2, 30*time.Second)
		}
	}()
	return c, nil
}
func (c *Client) Ready() bool { return c.ready.Load() }
func (c *Client) Close() error {
	c.cancel()
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready.Store(false)
	if c.sdk != nil {
		return c.sdk.Close()
	}
	return nil
}

var periods = map[string]quote.Period{"1m": quote.PeriodOneMinute, "5m": quote.PeriodFiveMinute, "15m": quote.PeriodFifteenMinute, "30m": quote.PeriodThirtyMinute, "1d": quote.PeriodDay, "1w": quote.PeriodWeek, "1mo": quote.PeriodMonth, "1y": quote.PeriodYear}

// Indexes is deliberately explicit: SDK upgrades cannot silently change accepted API names.
var Indexes = map[string]quote.CalcIndex{"last_done": quote.CalcIndexLastDone, "change_val": quote.CalcIndexChangeVal, "change_rate": quote.CalcIndexChangeRate, "volume": quote.CalcIndexVolume, "turnover": quote.CalcIndexTurnover, "turnover_rate": quote.CalcIndexTurnoverRate, "total_market_value": quote.CalcIndexTotalMarketValue, "pe_ttm_ratio": quote.CalcIndexPeTTMRatio, "pb_ratio": quote.CalcIndexPbRatio, "dividend_ratio_ttm": quote.CalcIndexDividendRatioTTM}

func derive(v *quote.SecurityQuote) *model.SecurityQuote {
	out := mapSecurityQuote(v)
	if v.LastDone != nil && v.PrevClose != nil {
		change := v.LastDone.Sub(*v.PrevClose)
		s := change.StringFixed(max(0, max(-v.LastDone.Exponent(), -v.PrevClose.Exponent())))
		out.Change = &s
		if v.PrevClose.IsPositive() {
			p := change.Mul(decimal.NewFromInt(100)).DivRound(*v.PrevClose, 2).StringFixed(2)
			out.ChangePercent = &p
		}
	}
	return out
}
func (c *Client) Fetch(ctx context.Context, qs []model.Query) map[string]model.Result {
	out := map[string]model.Result{}
	c.mu.RLock()
	sdk := c.sdk
	c.mu.RUnlock()
	if sdk == nil {
		for _, q := range qs {
			out[q.Key()] = model.Result{Err: model.E("UPSTREAM_UNAVAILABLE", 503)}
		}
		return out
	}
	put := func(q model.Query, v any) {
		b, e := json.Marshal(v)
		r := model.Result{Data: b}
		if e != nil {
			r.Err = model.E("UPSTREAM_ERROR", 502)
		}
		out[q.Key()] = r
	}
	q := qs[0]
	symbols := make([]string, len(qs))
	bySymbol := map[string]model.Query{}
	for i, q := range qs {
		symbols[i] = q.Symbol
		bySymbol[q.Symbol] = q
	}
	var err error
	switch q.Kind {
	case "quotes":
		var rows []*quote.SecurityQuote
		rows, err = sdk.Quote(ctx, symbols)
		for _, v := range rows {
			if v != nil {
				if q, ok := bySymbol[v.Symbol]; ok {
					put(q, derive(v))
				}
			}
		}
	case "securities":
		var rows []*quote.StaticInfo
		rows, err = sdk.StaticInfo(ctx, symbols)
		for _, v := range rows {
			if v != nil {
				if q, ok := bySymbol[v.Symbol]; ok {
					put(q, mapStaticInfo(v))
				}
			}
		}
	case "calc-indexes":
		indexes := []quote.CalcIndex{}
		for _, i := range strings.Split(q.Indexes, ",") {
			indexes = append(indexes, Indexes[i])
		}
		var rows []*quote.SecurityCalcIndex
		rows, err = sdk.CalcIndex(ctx, symbols, indexes)
		for _, v := range rows {
			if v != nil {
				if q, ok := bySymbol[v.Symbol]; ok {
					put(q, mapSecurityCalcIndex(v))
				}
			}
		}
	case "candlesticks", "history":
		adjust := quote.AdjustTypeNo
		if q.Adjust == "forward" {
			adjust = quote.AdjustTypeForward
		}
		var rows []*quote.Candlestick
		if q.Kind == "history" {
			start, _ := time.Parse(time.DateOnly, q.Start)
			end, _ := time.Parse(time.DateOnly, q.End)
			rows, err = sdk.HistoryCandlesticksByDate(ctx, q.Symbol, periods[q.Period], adjust, &start, &end, quote.CandlestickRequestTradeSession(quote.CandlestickTradeSessionNormal))
		} else {
			rows, err = sdk.Candlesticks(ctx, q.Symbol, periods[q.Period], int32(q.Count), adjust)
		}
		mapped := mapSlice(rows, mapCandlestick)
		sort.Slice(mapped, func(i, j int) bool { return mapped[i].Timestamp < mapped[j].Timestamp })
		put(q, mapped)
	case "depth":
		var v *quote.SecurityDepth
		v, err = sdk.Depth(ctx, q.Symbol)
		put(q, mapSecurityDepth(v))
	case "trades":
		var v []*quote.Trade
		v, err = sdk.Trades(ctx, q.Symbol, int32(q.Count))
		put(q, mapSlice(v, mapTrade))
	case "intraday":
		var v []*quote.IntradayLine
		v, err = sdk.Intraday(ctx, q.Symbol)
		put(q, mapSlice(v, mapIntradayLine))
	case "trading-sessions":
		var v []*quote.MarketTradingSession
		v, err = sdk.TradingSession(ctx)
		put(q, mapSlice(v, mapMarketTradingSession))
	case "trading-days":
		start, _ := time.Parse(time.DateOnly, q.Start)
		end, _ := time.Parse(time.DateOnly, q.End)
		var v *quote.MarketTradingDay
		v, err = sdk.TradingDays(ctx, openapi.Market(q.Market), &start, &end)
		if v != nil {
			dates := func(in []time.Time) []string {
				a := make([]string, len(in))
				for i, t := range in {
					a[i] = t.Format(time.DateOnly)
				}
				return a
			}
			put(q, struct {
				TradeDay     []string `json:"trade_day"`
				HalfTradeDay []string `json:"half_trade_day"`
			}{dates(v.TradeDay), dates(v.HalfTradeDay)})
		}
	}
	if err != nil {
		mapped := endpointError(err, q.Kind)
		if mapped.Code == "UPSTREAM_UNAVAILABLE" {
			c.ready.Store(false)
		}
		for _, q := range qs {
			out[q.Key()] = model.Result{Err: mapped}
		}
	} else {
		c.ready.Store(true)
	}
	return out
}
func mapError(err error) *model.Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return model.E("UPSTREAM_TIMEOUT", 504)
	}
	var n net.Error
	if errors.As(err, &n) || errors.Is(err, context.Canceled) {
		return model.E("UPSTREAM_UNAVAILABLE", 503)
	}
	return mapSDKError(err)
}
