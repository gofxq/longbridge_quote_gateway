package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/longbridge_quote_gateway/internal/cache"
	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/model"
	"github.com/gofxq/longbridge_quote_gateway/internal/service"
)

type testProvider struct{}

func (testProvider) Ready() bool  { return true }
func (testProvider) Close() error { return nil }
func (testProvider) Fetch(_ context.Context, qs []model.Query) map[string]model.Result {
	out := map[string]model.Result{}
	for _, q := range qs {
		if q.Symbol == "BAD.US" {
			out[q.Key()] = model.Result{Err: model.E("MARKET_DATA_PERMISSION_DENIED", 403)}
		} else {
			b, _ := json.Marshal(map[string]string{"symbol": q.Symbol})
			out[q.Key()] = model.Result{Data: b}
		}
	}
	return out
}
func TestHTTPContract(t *testing.T) {
	t.Setenv("GATEWAY_TOKEN", strings.Repeat("a", 64))
	cfg, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	s := service.New(context.Background(), testProvider{}, cache.New(time.Minute, 100, 100000, 10000), cfg)
	defer s.Close()
	h := New(cfg, s, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, tc := range []struct {
		url, token, code string
		status           int
	}{{"/api/v1/quotes?symbols=A.US", "", "UNAUTHORIZED", 401}, {"/api/v1/quotes?symbols=A.US", "bad", "UNAUTHORIZED", 401}, {"/api/v1/quotes?symbols= a.us ,A.US,BAD.US", cfg.Token, "PARTIAL_SUCCESS", 200}, {"/api/v1/quotes?symbols=BAD.US", cfg.Token, "BATCH_FAILED", 502}, {"/api/v1/quotes/BAD.US", cfg.Token, "MARKET_DATA_PERMISSION_DENIED", 403}, {"/api/v1/quotes/A.US", cfg.Token, "OK", 200}, {"/api/v1/quotes?symbols=A.US&force_refresh=true", cfg.Token, "INVALID_PARAMETER", 400}, {"/api/v1/candlesticks?symbol=A.US&period=1M", cfg.Token, "INVALID_PARAMETER", 400}, {"/api/v1/quotes?symbols=A.US,,B.US", cfg.Token, "INVALID_PARAMETER", 400}} {
		t.Run(tc.url, func(t *testing.T) {
			r := httptest.NewRequest("GET", strings.ReplaceAll(tc.url, " ", "%20"), nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			var env map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || env["code"] != tc.code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			for _, k := range []string{"code", "message", "data", "errors", "request_id", "meta"} {
				if _, ok := env[k]; !ok {
					t.Fatal("missing", k)
				}
			}
		})
	}
}
func TestHistoryMetadata(t *testing.T) {
	for _, n := range []int{0, 999, 1000} {
		rows := make([]model.Candlestick, n)
		for i := range rows {
			rows[i].Timestamp = int64(i + 1)
		}
		b, _ := json.Marshal(rows)
		env, _ := assemble([]model.Query{{Kind: "history", Symbol: "A.US", Start: "2026-01-01", End: "2026-02-01"}}, []model.Result{{Data: b, Meta: model.CacheMeta{Hit: true}}}, false)
		if env.Meta["possibly_truncated"] != (n >= 1000) || env.Meta["returned_count"] != n {
			t.Fatal(env)
		}
		if n == 0 && env.Meta["returned_start"] != nil {
			t.Fatal(env)
		}
	}
}
func TestValidationNormalization(t *testing.T) {
	t.Setenv("GATEWAY_TOKEN", strings.Repeat("a", 64))
	cfg, _ := config.Load()
	for _, raw := range []string{"symbols=A.US,A.US,A.US", "symbols=A.SG", "symbols=", "symbols=A.US&symbols=B.US"} {
		cfg.BatchSize = 2
		r := httptest.NewRequest("GET", "/?"+raw, nil)
		_, _, err := parseQuery("quotes", "", r.URL.Query(), cfg)
		if err == nil {
			t.Fatal(raw)
		}
	}
}

type blockingProvider struct{ started, release chan struct{} }

func (b *blockingProvider) Ready() bool  { return true }
func (b *blockingProvider) Close() error { return nil }
func (b *blockingProvider) Fetch(ctx context.Context, qs []model.Query) map[string]model.Result {
	close(b.started)
	select {
	case <-ctx.Done():
	case <-b.release:
	}
	return testProvider{}.Fetch(ctx, qs)
}
func TestHTTPInflightCapacity(t *testing.T) {
	t.Setenv("GATEWAY_TOKEN", strings.Repeat("a", 64))
	cfg, _ := config.Load()
	cfg.HTTPInflight = 1
	p := &blockingProvider{make(chan struct{}), make(chan struct{})}
	s := service.New(context.Background(), p, cache.New(time.Minute, 100, 100000, 10000), cfg)
	defer s.Close()
	h := New(cfg, s, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/quotes/A.US", nil)
		r.Header.Set("Authorization", "Bearer "+cfg.Token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	done := make(chan struct{})
	go func() { request(); close(done) }()
	<-p.started
	w := request()
	if w.Code != 503 || !strings.Contains(w.Body.String(), "REQUEST_CAPACITY_EXCEEDED") {
		t.Fatal(w.Code, w.Body.String())
	}
	close(p.release)
	<-done
}
