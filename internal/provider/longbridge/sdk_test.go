package longbridge

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdkconfig "github.com/longbridge/openapi-go/config"
	"github.com/longbridge/openapi-go/quote"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSDKCalcIndexPreservesTurnover(t *testing.T) {
	cfg, err := sdkconfig.New(sdkconfig.WithConfigKey("test", "test", "test"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.QuoteTransport = "http"
	cfg.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"security_calc_index":[{"symbol":"A.US","turnover":"1234.50","turnover_rate":"0.25"}]}}`))}, nil
	})}
	sdk, err := quote.NewFromCfg(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	rows, err := sdk.CalcIndex(context.Background(), []string{"A.US"}, []quote.CalcIndex{quote.CalcIndexTurnover, quote.CalcIndexTurnoverRate})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turnover == nil || rows[0].Turnover.String() != "1234.5" {
		t.Fatalf("turnover corrupted: %+v", rows)
	}
}
func TestSDKRateLimitRetryHonorsContext(t *testing.T) {
	cfg, err := sdkconfig.New(sdkconfig.WithConfigKey("test", "test", "test"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.QuoteTransport = "http"
	cfg.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"code":459101,"message":"limited"}`))}, nil
	})}
	sdk, err := quote.NewFromCfg(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = sdk.Quote(ctx, []string{"A.US"})
	if err == nil || time.Since(start) > 300*time.Millisecond {
		t.Fatal("SDK retry ignored deadline", err)
	}
}
