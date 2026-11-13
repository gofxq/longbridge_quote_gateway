package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr, Token, LogLevel                                                                                                                            string
	HTTPTimeout, TTL, UpstreamTimeout, QueueTimeout, BatchWindow                                                                                     time.Duration
	HTTPInflight, CacheEntries, CacheBytes, CacheItemBytes, Concurrent, QueueSize, PendingKeys, BatchSize, MaxCandles, RPS, IngressRPS, IngressBurst int
}

func Load() (Config, error) {
	c := Config{Addr: "127.0.0.1:12280", Token: os.Getenv("GATEWAY_TOKEN"), LogLevel: "info"}
	for k, p := range map[string]*string{"HTTP_ADDR": &c.Addr, "LOG_LEVEL": &c.LogLevel} {
		if v, ok := os.LookupEnv(k); ok {
			*p = v
		}
	}
	for _, x := range []struct {
		k string
		p *int
		d int
	}{{"HTTP_MAX_INFLIGHT", &c.HTTPInflight, 128}, {"CACHE_MAX_ENTRIES", &c.CacheEntries, 20000}, {"CACHE_MAX_BYTES", &c.CacheBytes, 67108864}, {"CACHE_MAX_ITEM_BYTES", &c.CacheItemBytes, 2097152}, {"UPSTREAM_MAX_CONCURRENT", &c.Concurrent, 4}, {"UPSTREAM_QUEUE_SIZE", &c.QueueSize, 32}, {"UPSTREAM_MAX_PENDING_KEYS", &c.PendingKeys, 512}, {"MAX_BATCH_SYMBOLS", &c.BatchSize, 50}, {"MAX_CANDLESTICKS", &c.MaxCandles, 1000}, {"UPSTREAM_RPS", &c.RPS, 8}, {"INGRESS_RPS", &c.IngressRPS, 30}, {"INGRESS_BURST", &c.IngressBurst, 60}} {
		*x.p = x.d
		if v, ok := os.LookupEnv(x.k); ok {
			n, e := strconv.Atoi(v)
			if e != nil {
				return c, fmt.Errorf("invalid %s", x.k)
			}
			*x.p = n
		}
		if *x.p <= 0 {
			return c, fmt.Errorf("%s must be positive", x.k)
		}
	}
	for _, x := range []struct {
		k string
		p *time.Duration
		d time.Duration
	}{{"HTTP_TIMEOUT", &c.HTTPTimeout, 10 * time.Second}, {"CACHE_TTL", &c.TTL, 60 * time.Second}, {"UPSTREAM_TIMEOUT", &c.UpstreamTimeout, 8 * time.Second}, {"UPSTREAM_QUEUE_TIMEOUT", &c.QueueTimeout, time.Second}, {"BATCH_WINDOW", &c.BatchWindow, 5 * time.Millisecond}} {
		*x.p = x.d
		if v, ok := os.LookupEnv(x.k); ok {
			d, e := time.ParseDuration(v)
			if e != nil {
				return c, fmt.Errorf("invalid %s", x.k)
			}
			*x.p = d
		}
		if *x.p <= 0 {
			return c, fmt.Errorf("%s must be positive", x.k)
		}
	}
	if len(c.Token) < 32 {
		return c, fmt.Errorf("GATEWAY_TOKEN must contain at least 32 bytes")
	}
	if c.RPS > 10 || c.Concurrent > 5 || c.BatchSize > 500 || c.MaxCandles > 1000 || c.CacheItemBytes > c.CacheBytes || c.QueueTimeout >= c.UpstreamTimeout {
		return c, fmt.Errorf("configuration exceeds upstream limits or has inconsistent budgets")
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return c, fmt.Errorf("invalid LOG_LEVEL")
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" && v != "json" {
		return c, fmt.Errorf("LOG_FORMAT must be json")
	}
	return c, nil
}
