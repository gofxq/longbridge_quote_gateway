// Package service owns key-level singleflight, microbatching and bounded scheduling.
package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/gofxq/longbridge_quote_gateway/internal/cache"
	"github.com/gofxq/longbridge_quote_gateway/internal/config"
	"github.com/gofxq/longbridge_quote_gateway/internal/model"
	"github.com/gofxq/longbridge_quote_gateway/internal/provider"
)

type ticket struct {
	query   model.Query
	created time.Time
	started bool
	done    chan struct{}
	result  model.Result
}
type Service struct {
	mu                      sync.Mutex
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
	p                       provider.Provider
	cache                   *cache.Cache
	cfg                     config.Config
	pending                 map[string]*ticket
	window                  map[string][]*ticket
	queue                   chan []*ticket
	queued                  int
	limiter                 *rate.Limiter
	calls, errors, inflight atomic.Int64
	duration                atomic.Int64
}

func New(ctx context.Context, p provider.Provider, c *cache.Cache, cfg config.Config) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{ctx: ctx, cancel: cancel, p: p, cache: c, cfg: cfg, pending: map[string]*ticket{}, window: map[string][]*ticket{}, queue: make(chan []*ticket, cfg.QueueSize), limiter: rate.NewLimiter(rate.Limit(cfg.RPS), 1)}
	s.wg.Add(1)
	go s.batchLoop()
	for i := 0; i < cfg.Concurrent; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}
func completed(r model.Result) *ticket {
	t := &ticket{done: make(chan struct{}), result: r}
	close(t.done)
	return t
}
func (s *Service) register(q model.Query) *ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return completed(model.Result{Err: model.E("UPSTREAM_UNAVAILABLE", 503)})
	}
	if r, ok := s.cache.Get(q.Key()); ok {
		return completed(r)
	}
	if t := s.pending[q.Key()]; t != nil {
		return t
	}
	if len(s.pending) >= s.cfg.PendingKeys {
		return completed(model.Result{Err: model.E("UPSTREAM_QUEUE_FULL", 503)})
	}
	t := &ticket{query: q, created: time.Now(), done: make(chan struct{})}
	s.pending[q.Key()] = t
	group := q.Group()
	if !q.Batchable() {
		group = q.Key()
	}
	s.window[group] = append(s.window[group], t)
	if len(s.window[group]) >= s.cfg.BatchSize || !q.Batchable() {
		s.enqueue(s.window[group])
		delete(s.window, group)
	}
	return t
}

// All registration and publication happen under mu; no network call holds mu.
func (s *Service) finish(t *ticket, r model.Result) {
	if s.pending[t.query.Key()] != t {
		return
	}
	if r.Err == nil && !r.Meta.Hit {
		r.Meta = s.cache.Put(t.query.Key(), r.Data)
	}
	t.result = r
	delete(s.pending, t.query.Key())
	close(t.done)
}
func (s *Service) enqueue(batch []*ticket) {
	if s.queued >= s.cfg.QueueSize {
		for _, t := range batch {
			s.finish(t, model.Result{Err: model.E("UPSTREAM_QUEUE_FULL", 503)})
		}
		return
	}
	s.queued++
	s.queue <- batch
}
func (s *Service) batchLoop() {
	defer s.wg.Done()
	timer := time.NewTicker(min(s.cfg.BatchWindow, 10*time.Millisecond))
	lastFlush := time.Now()
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.mu.Lock()
			now := time.Now()
			for _, t := range s.pending {
				if !t.started && !now.Before(t.created.Add(s.cfg.QueueTimeout)) {
					s.finish(t, model.Result{Err: model.E("UPSTREAM_QUEUE_TIMEOUT", 503)})
				}
			}
			if now.Sub(lastFlush) >= s.cfg.BatchWindow {
				for k, b := range s.window {
					live := b[:0]
					for _, t := range b {
						if s.pending[t.query.Key()] == t {
							live = append(live, t)
						}
					}
					if len(live) > 0 {
						s.enqueue(live)
					}
					delete(s.window, k)
				}
				lastFlush = now
			}
			s.mu.Unlock()
		}
	}
}
func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case batch := <-s.queue:
			s.execute(batch)
		}
	}
}
func (s *Service) execute(batch []*ticket) {
	first := batch[0].created
	for _, t := range batch {
		if t.created.Before(first) {
			first = t.created
		}
	}
	s.mu.Lock()
	live := make([]*ticket, 0, len(batch))
	for _, t := range batch {
		if s.pending[t.query.Key()] == t {
			live = append(live, t)
		}
	}
	if len(live) == 0 {
		s.queued--
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	batch = live
	schedule, cancel := context.WithDeadline(s.ctx, first.Add(s.cfg.QueueTimeout))
	err := s.limiter.Wait(schedule)
	cancel()
	if err == nil && !time.Now().Before(first.Add(s.cfg.QueueTimeout)) {
		err = context.DeadlineExceeded
	}
	s.mu.Lock()
	s.queued--
	if err == nil {
		for _, t := range batch {
			t.started = true
		}
	}
	s.mu.Unlock()
	results := map[string]model.Result{}
	if err != nil {
		for _, t := range batch {
			results[t.query.Key()] = model.Result{Err: model.E("UPSTREAM_QUEUE_TIMEOUT", 503)}
		}
	} else {
		ctx, cancel := context.WithDeadline(s.ctx, first.Add(s.cfg.UpstreamTimeout))
		queries := make([]model.Query, len(batch))
		for i, t := range batch {
			queries[i] = t.query
		}
		start := time.Now()
		s.calls.Add(1)
		s.inflight.Add(1)
		results = s.p.Fetch(ctx, queries)
		s.inflight.Add(-1)
		s.duration.Add(time.Since(start).Nanoseconds())
		if ctx.Err() != nil {
			code, status := "UPSTREAM_TIMEOUT", 504
			if s.ctx.Err() != nil {
				code, status = "UPSTREAM_UNAVAILABLE", 503
			}
			for _, t := range batch {
				results[t.query.Key()] = model.Result{Err: model.E(code, status)}
			}
		}
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range batch {
		r, ok := results[t.query.Key()]
		if !ok {
			r.Err = model.E("SYMBOL_DATA_MISSING", 502)
		}
		if r.Err != nil {
			s.errors.Add(1)
		}
		s.finish(t, r)
	}
}
func (s *Service) Query(ctx context.Context, queries []model.Query) []model.Result {
	tickets := make([]*ticket, len(queries))
	for i, q := range queries {
		tickets[i] = s.register(q)
	}
	results := make([]model.Result, len(queries))
	for i, t := range tickets {
		select {
		case <-t.done:
			results[i] = t.result
		default:
			select {
			case <-t.done:
				results[i] = t.result
			case <-ctx.Done():
				select {
				case <-t.done:
					results[i] = t.result
				default:
					results[i].Err = model.E("REQUEST_TIMEOUT", 504)
				}
			}
		}
	}
	return results
}
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.pending {
		s.finish(t, model.Result{Err: model.E("UPSTREAM_UNAVAILABLE", 503)})
	}
	s.window = map[string][]*ticket{}
}
func (s *Service) Ready() bool { return s.ctx.Err() == nil && s.p.Ready() }
func (s *Service) Stats() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cache.Stats()
	m["upstream_pending_keys"] = uint64(len(s.pending))
	m["upstream_queue_length"] = uint64(s.queued)
	m["upstream_requests_total"] = uint64(s.calls.Load())
	m["upstream_errors_total"] = uint64(s.errors.Load())
	m["upstream_inflight"] = uint64(s.inflight.Load())
	m["upstream_request_duration_nanoseconds_total"] = uint64(s.duration.Load())
	if s.Ready() {
		m["upstream_connected"] = 1
	} else {
		m["upstream_connected"] = 0
	}
	return m
}
