package config

import (
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	t.Setenv("GATEWAY_TOKEN", strings.Repeat("a", 64))
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
	for k, v := range map[string]string{"UPSTREAM_RPS": "11", "UPSTREAM_MAX_CONCURRENT": "6", "MAX_BATCH_SYMBOLS": "501", "MAX_CANDLESTICKS": "1001", "CACHE_TTL": "0s", "CACHE_MAX_ITEM_BYTES": "999999999", "HTTP_MAX_INFLIGHT": "-1"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if _, e := Load(); e == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
