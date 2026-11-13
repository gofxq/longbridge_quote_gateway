package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gofxq/longbridge_quote_gateway/internal/cache"
	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

type fake struct {
	mu    sync.Mutex
	calls int
	keys  map[string]int
	delay time.Duration
	block chan struct{}
	omit  string
	fail  bool
}

func (f *fake) Ready() bool  { return true }
func (f *fake) Close() error { return nil }
func (f *fake) Fetch(ctx context.Context, qs []model.Query) map[string]model.Result {
	f.mu.Lock()
	f.calls++
	for _, q := range qs {
		f.keys[q.Symbol]++
	}
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-ctx.Done():
		case <-f.block:
		}
	}
	if f.delay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(f.delay):
		}
	}
	out := map[string]model.Result{}
	for _, q := range qs {
		if q.Symbol == f.omit {
			continue
		}
		if f.fail {
			out[q.Key()] = model.Result{Err: model.E("UPSTREAM_TIMEOUT", 504)}
			continue
		}
		b, _ := json.Marshal(map[string]string{"symbol": q.Symbol})
		out[q.Key()] = model.Result{Data: b}
	}
	return out
}
func setup(t *testing.T, edit func(*config.Config), f *fake) *Service {
	t.Helper()
	cfg := config.Config{TTL: time.Minute, Concurrent: 4, QueueSize: 32, PendingKeys: 512, BatchSize: 50, RPS: 1000, BatchWindow: 5 * time.Millisecond, QueueTimeout: time.Second, UpstreamTimeout: 2 * time.Second}
	if edit != nil {
		edit(&cfg)
	}
	f.keys = map[string]int{}
	s := New(context.Background(), f, cache.New(cfg.TTL, 1000, 1000000, 10000), cfg)
	t.Cleanup(s.Close)
	return s
}
func q(symbol string) model.Query { return model.Query{Kind: "quotes", Symbol: symbol} }
func TestConcurrentOverlappingBatches(t *testing.T) {
	f := &fake{delay: 20 * time.Millisecond}
	s := setup(t, nil, f)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			qs := []model.Query{q("A.US"), q("B.US")}
			if i%2 == 0 {
				qs = []model.Query{q("B.US"), q("C.US")}
			}
			for _, r := range s.Query(context.Background(), qs) {
				if r.Err != nil {
					t.Error(r.Err)
				}
			}
		}(i)
	}
	wg.Wait()
	for _, symbol := range []string{"A.US", "B.US", "C.US"} {
		if f.keys[symbol] != 1 {
			t.Fatal(f.keys)
		}
	}
	if r := s.Query(context.Background(), []model.Query{q("B.US")})[0]; !r.Meta.Hit {
		t.Fatal("cache not shared")
	}
}
func TestCancellationDoesNotCancelSharedTask(t *testing.T) {
	f := &fake{delay: 50 * time.Millisecond}
	s := setup(t, nil, f)
	ctx, cancel := context.WithCancel(context.Background())
	first := s.register(q("A.US"))
	cancel()
	r := s.Query(ctx, []model.Query{q("A.US")})[0]
	if r.Err == nil {
		t.Fatal("cancellation ignored")
	}
	r = s.Query(context.Background(), []model.Query{q("A.US")})[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	<-first.done
	if f.calls != 1 {
		t.Fatal(f.calls)
	}
}
func TestCapacityAndDeadlineRelease(t *testing.T) {
	f := &fake{block: make(chan struct{})}
	s := setup(t, func(c *config.Config) {
		c.PendingKeys = 1
		c.UpstreamTimeout = 50 * time.Millisecond
		c.QueueTimeout = 20 * time.Millisecond
	}, f)
	first := s.register(q("A.US"))
	r := s.Query(context.Background(), []model.Query{q("B.US")})[0]
	if r.Err.Code != "UPSTREAM_QUEUE_FULL" {
		t.Fatal(r)
	}
	<-first.done
	if first.result.Err.Code != "UPSTREAM_TIMEOUT" {
		t.Fatal(first.result)
	}
	if s.Stats()["upstream_pending_keys"] != 0 {
		t.Fatal("registration leaked")
	}
}
func TestQueuedDeadlineDoesNotWaitForRunningCall(t *testing.T) {
	f := &fake{block: make(chan struct{})}
	s := setup(t, func(c *config.Config) {
		c.Concurrent = 1
		c.QueueTimeout = 35 * time.Millisecond
		c.UpstreamTimeout = time.Second
	}, f)
	first := s.register(q("A.US"))
	time.Sleep(15 * time.Millisecond)
	start := time.Now()
	r := s.Query(context.Background(), []model.Query{q("B.US")})[0]
	if r.Err == nil || r.Err.Code != "UPSTREAM_QUEUE_TIMEOUT" {
		t.Fatal(r)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("queue timeout waited for worker")
	}
	close(f.block)
	<-first.done
}
func TestMissingAndFailedNotCached(t *testing.T) {
	f := &fake{omit: "B.US"}
	s := setup(t, nil, f)
	r := s.Query(context.Background(), []model.Query{q("A.US"), q("B.US")})
	if r[0].Err != nil || r[1].Err.Code != "SYMBOL_DATA_MISSING" {
		t.Fatal(r)
	}
	s.Query(context.Background(), []model.Query{q("B.US")})
	if f.keys["B.US"] != 2 {
		t.Fatal("error cached")
	}
}
func TestShutdownWakesWaiters(t *testing.T) {
	f := &fake{block: make(chan struct{})}
	s := setup(t, nil, f)
	ticket := s.register(q("A.US"))
	s.Close()
	select {
	case <-ticket.done:
	case <-time.After(time.Second):
		t.Fatal("waiter leaked")
	}
}

func TestQueueFullAndNoRegistrationLeak(t *testing.T) {
	f := &fake{block: make(chan struct{})}
	s := setup(t, func(c *config.Config) {
		c.Concurrent = 1
		c.QueueSize = 1
		c.BatchSize = 1
		c.QueueTimeout = 200 * time.Millisecond
	}, f)
	first := s.register(q("A.US"))
	deadline := time.Now().Add(time.Second)
	for s.inflight.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second := s.register(q("B.US"))
	third := s.register(q("C.US"))
	<-third.done
	if third.result.Err == nil || third.result.Err.Code != "UPSTREAM_QUEUE_FULL" {
		t.Fatal(third.result)
	}
	close(f.block)
	<-first.done
	<-second.done
	if s.Stats()["upstream_pending_keys"] != 0 {
		t.Fatal("pending leak")
	}
}
func TestRateLimitAndCachedRequests(t *testing.T) {
	f := &fake{}
	s := setup(t, func(c *config.Config) { c.RPS = 10; c.BatchSize = 1 }, f)
	start := time.Now()
	s.Query(context.Background(), []model.Query{q("A.US"), q("B.US"), q("C.US")})
	if time.Since(start) < 190*time.Millisecond {
		t.Fatal("rate exceeded")
	}
	before := f.calls
	s.Query(context.Background(), []model.Query{q("A.US"), q("B.US")})
	if f.calls != before {
		t.Fatal("cache called upstream")
	}
}
