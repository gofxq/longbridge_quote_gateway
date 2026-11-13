package server

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,39}\.(US|HK|SH|SZ)$`)
var validPeriods = map[string]bool{"1m": true, "5m": true, "15m": true, "30m": true, "1d": true, "1w": true, "1mo": true, "1y": true}
var validIndexes = map[string]bool{"last_done": true, "change_val": true, "change_rate": true, "volume": true, "turnover": true, "turnover_rate": true, "total_market_value": true, "pe_ttm_ratio": true, "pb_ratio": true, "dividend_ratio_ttm": true}

func parseQuery(kind, pathSymbol string, v url.Values, cfg config.Config) ([]model.Query, bool, *model.Error) {
	bad := func() ([]model.Query, bool, *model.Error) { return nil, false, model.E("INVALID_PARAMETER", 400) }
	q := model.Query{Kind: kind}
	allowed := map[string]bool{}
	allow := func(keys ...string) {
		for _, k := range keys {
			allowed[k] = true
		}
	}
	batch := q.Batchable() && pathSymbol == ""
	if batch {
		allow("symbols")
	} else if kind != "trading-days" && kind != "trading-sessions" && pathSymbol == "" {
		allow("symbol")
	}
	switch kind {
	case "candlesticks", "history":
		allow("period", "adjust")
		q.Period = v.Get("period")
		if q.Period == "" {
			q.Period = "1d"
		}
		q.Adjust = v.Get("adjust")
		if q.Adjust == "" {
			q.Adjust = "none"
		}
		if !validPeriods[q.Period] || (q.Adjust != "none" && q.Adjust != "forward") {
			return bad()
		}
	}
	if kind == "candlesticks" || kind == "trades" {
		allow("count")
		q.Count = 100
		maxCount := 1000
		if kind == "candlesticks" {
			maxCount = cfg.MaxCandles
		}
		if val, ok := v["count"]; ok {
			n, e := strconv.Atoi(val[0])
			if e != nil {
				return bad()
			}
			q.Count = n
		}
		if q.Count < 1 || q.Count > maxCount {
			return bad()
		}
	}
	if kind == "history" || kind == "trading-days" {
		allow("start", "end")
		q.Start = v.Get("start")
		q.End = v.Get("end")
		start, e := time.Parse(time.DateOnly, q.Start)
		end, e2 := time.Parse(time.DateOnly, q.End)
		if e != nil || e2 != nil || start.After(end) {
			return bad()
		}
		if kind == "trading-days" {
			allow("market")
			q.Market = strings.ToUpper(v.Get("market"))
			if q.Market != "US" && q.Market != "HK" && q.Market != "CN" {
				return bad()
			}
			if end.Sub(start) > 365*24*time.Hour {
				return bad()
			}
		}
	}
	if kind == "calc-indexes" {
		allow("indexes")
		set := map[string]bool{}
		for _, i := range strings.Split(v.Get("indexes"), ",") {
			i = strings.TrimSpace(i)
			if !validIndexes[i] {
				return bad()
			}
			set[i] = true
		}
		a := []string{}
		for i := range set {
			a = append(a, i)
		}
		sort.Strings(a)
		q.Indexes = strings.Join(a, ",")
	}
	for k, values := range v {
		if !allowed[k] || len(values) != 1 || values[0] == "" {
			return bad()
		}
	}
	if kind == "trading-days" || kind == "trading-sessions" {
		return []model.Query{q}, false, nil
	}
	raw := pathSymbol
	if raw == "" {
		raw = v.Get("symbol")
	}
	if batch {
		raw = v.Get("symbols")
	}
	parts := strings.Split(raw, ",")
	if len(parts) > cfg.BatchSize || (!batch && len(parts) != 1) {
		return bad()
	}
	queries := []model.Query{}
	seen := map[string]bool{}
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if !symbolPattern.MatchString(symbol) {
			return bad()
		}
		if !seen[symbol] {
			item := q
			item.Symbol = symbol
			queries = append(queries, item)
			seen[symbol] = true
		}
	}
	return queries, batch, nil
}
