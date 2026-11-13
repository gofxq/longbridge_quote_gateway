package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/model"
	"github.com/gofxq/longbridge_quote_gateway/internal/service"
)

type API struct {
	ips                *ipLimits
	cfg                config.Config
	svc                *service.Service
	log                *slog.Logger
	slots              chan struct{}
	ingress            *rate.Limiter
	requests, duration atomic.Int64
}

func New(cfg config.Config, svc *service.Service, logger *slog.Logger) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	a := &API{ips: &ipLimits{entries: map[string]ipEntry{}, rps: cfg.IngressRPS, burst: cfg.IngressBurst}, cfg: cfg, svc: svc, log: logger, slots: make(chan struct{}, cfg.HTTPInflight), ingress: rate.NewLimiter(rate.Limit(cfg.IngressRPS), cfg.IngressBurst)}
	r.Use(func(c *gin.Context) {
		start := time.Now()
		id := make([]byte, 16)
		_, _ = rand.Read(id)
		c.Set("request_id", hex.EncodeToString(id))
		c.Header("X-Request-ID", hex.EncodeToString(id))
		defer func() {
			if recover() != nil {
				a.fail(c, model.E("UPSTREAM_ERROR", 502))
			}
			a.requests.Add(1)
			a.duration.Add(time.Since(start).Nanoseconds())
			path := c.FullPath()
			if path == "" {
				path = "unmatched"
			}
			logger.Info("request", "request_id", c.GetString("request_id"), "method", c.Request.Method, "path", path, "status", c.Writer.Status(), "latency_ms", time.Since(start).Milliseconds())
		}()
		ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.HTTPTimeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if !svc.Ready() {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Data(200, "text/html; charset=utf-8", []byte(`<!doctype html><html><head><title>Quote Gateway API</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.30.2/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5.30.2/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({dom_id:'#swagger-ui',spec:`+string(OpenAPI)+`,persistAuthorization:false});</script></body></html>`))
	})
	protected := r.Group("", a.auth)
	protected.GET("/metrics", func(c *gin.Context) {
		m := svc.Stats()
		m["http_requests_total"] = uint64(a.requests.Load())
		m["http_request_duration_nanoseconds_total"] = uint64(a.duration.Load())
		keys := []string{}
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %d\n", k, m[k])
		}
		fmt.Fprintf(&b, "http_request_duration_seconds_sum %g\nhttp_request_duration_seconds_count %d\nupstream_request_duration_seconds_sum %g\nupstream_request_duration_seconds_count %d\n", float64(a.duration.Load())/1e9, a.requests.Load(), float64(m["upstream_request_duration_nanoseconds_total"])/1e9, m["upstream_requests_total"])
		c.Data(200, "text/plain; version=0.0.4", []byte(b.String()))
	})
	protected.GET("/openapi.json", func(c *gin.Context) { c.Data(200, "application/json", OpenAPI) })
	api := protected.Group("/api/v1", a.admit)
	for _, kind := range []string{"quotes", "securities", "candlesticks", "history", "intraday", "depth", "trades", "calc-indexes", "trading-sessions", "trading-days"} {
		path := kind
		if kind == "history" {
			path = "candlesticks/history"
		}
		api.GET("/"+path, a.handle(kind))
	}
	api.GET("/quotes/:symbol", a.handle("quotes"))
	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, model.Envelope{Code: "NOT_FOUND", Message: "endpoint not found", Errors: []model.Error{}, RequestID: c.GetString("request_id"), Meta: map[string]any{"cache_hit": false}})
	})
	return r
}
func (a *API) auth(c *gin.Context) {
	h := c.GetHeader("Authorization")
	parts := strings.Split(h, " ")
	valid := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer")
	token := ""
	if valid {
		token = parts[1]
	}
	got, want := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(a.cfg.Token))
	if !valid || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		a.fail(c, model.E("UNAUTHORIZED", 401))
		return
	}
	c.Next()
}
func (a *API) admit(c *gin.Context) {
	if !a.ingress.Allow() || !a.ips.allow(c.ClientIP()) {
		a.fail(c, model.E("RATE_LIMITED", 429))
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		a.fail(c, model.E("REQUEST_CAPACITY_EXCEEDED", 503))
		return
	}
	c.Next()
}
func (a *API) fail(c *gin.Context, e *model.Error) {
	c.AbortWithStatusJSON(e.Status, model.Envelope{Code: e.Code, Message: e.Message, Errors: []model.Error{}, RequestID: c.GetString("request_id"), Meta: map[string]any{"cache_hit": false}})
}
func (a *API) handle(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		values, err := httpQuery(c)
		if err != nil {
			a.fail(c, model.E("INVALID_PARAMETER", 400))
			return
		}
		qs, batch, e := parseQuery(kind, c.Param("symbol"), values, a.cfg)
		if e != nil {
			a.fail(c, e)
			return
		}
		results := a.svc.Query(c.Request.Context(), qs)
		env, status := assemble(qs, results, batch)
		hits := 0
		for _, r := range results {
			if r.Meta.Hit {
				hits++
			}
		}
		a.log.Debug("query", "request_id", c.GetString("request_id"), "symbols_count", len(qs), "cache_hit_count", hits, "cache_miss_count", len(qs)-hits)
		env.RequestID = c.GetString("request_id")
		c.JSON(status, env)
	}
}
func assemble(qs []model.Query, results []model.Result, batch bool) (model.Envelope, int) {
	env := model.Envelope{Code: "OK", Message: "success", Errors: []model.Error{}, Meta: map[string]any{"cache_hit": true}}
	data := []json.RawMessage{}
	symbols := map[string]model.CacheMeta{}
	status := 200
	for i, r := range results {
		q := qs[i]
		if r.Err != nil {
			env.Meta["cache_hit"] = false
			if !batch {
				env.Code = r.Err.Code
				env.Message = r.Err.Message
				return env, r.Err.Status
			}
			e := *r.Err
			e.Symbol = q.Symbol
			env.Errors = append(env.Errors, e)
			continue
		}
		data = append(data, r.Data)
		if !r.Meta.Hit {
			env.Meta["cache_hit"] = false
		}
		if q.Symbol != "" {
			symbols[q.Symbol] = r.Meta
		} else {
			env.Meta["cache"] = r.Meta
		}
		if q.Kind == "history" {
			var candles []model.Candlestick
			_ = json.Unmarshal(r.Data, &candles)
			env.Meta["requested_start"] = q.Start
			env.Meta["requested_end"] = q.End
			env.Meta["returned_count"] = len(candles)
			env.Meta["possibly_truncated"] = len(candles) >= 1000
			env.Meta["returned_start"] = nil
			env.Meta["returned_end"] = nil
			if len(candles) > 0 {
				env.Meta["returned_start"] = time.Unix(candles[0].Timestamp, 0).UTC().Format(time.RFC3339)
				env.Meta["returned_end"] = time.Unix(candles[len(candles)-1].Timestamp, 0).UTC().Format(time.RFC3339)
			}
		}
	}
	if len(symbols) > 0 {
		env.Meta["symbols"] = symbols
	}
	if batch {
		env.Data = data
		if len(env.Errors) > 0 {
			env.Code = "PARTIAL_SUCCESS"
			env.Message = "some symbols failed"
			if len(data) == 0 {
				status = 502
				env.Code = "BATCH_FAILED"
				env.Message = "all symbols failed"
			}
		}
	} else if len(data) > 0 {
		env.Data = data[0]
	}
	return env, status
}
