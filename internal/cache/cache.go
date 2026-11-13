// Package cache provides an immutable payload TTL/LRU cache with byte accounting.
package cache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

type entry struct {
	key         string
	data        []byte
	at, expires time.Time
}
type Cache struct {
	mu                                      sync.Mutex
	items                                   map[string]*list.Element
	lru                                     *list.List
	ttl                                     time.Duration
	maxEntries, maxBytes, maxItem, bytes    int
	Hits, Misses, Expired, Evicted, Skipped uint64
}

func New(ttl time.Duration, entries, bytes, item int) *Cache {
	return &Cache{items: make(map[string]*list.Element), lru: list.New(), ttl: ttl, maxEntries: entries, maxBytes: bytes, maxItem: item}
}
func (c *Cache) remove(e *list.Element) {
	v := e.Value.(entry)
	delete(c.items, v.key)
	c.bytes -= len(v.key) + len(v.data)
	c.lru.Remove(e)
}
func (c *Cache) sweep(now time.Time) {
	for _, e := range c.items {
		if !now.Before(e.Value.(entry).expires) {
			c.remove(e)
			c.Expired++
		}
	}
}
func (c *Cache) meta(v entry, hit bool) model.CacheMeta {
	return model.CacheMeta{Hit: hit, Stored: true, CachedAt: &v.at, ExpiresAt: &v.expires, TTL: c.ttl.Seconds()}
}
func (c *Cache) Get(key string) (model.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.items[key]; e != nil {
		v := e.Value.(entry)
		if time.Now().Before(v.expires) {
			c.lru.MoveToFront(e)
			c.Hits++
			return model.Result{Data: append([]byte(nil), v.data...), Meta: c.meta(v, true)}, true
		}
		c.remove(e)
		c.Expired++
	}
	c.Misses++
	return model.Result{}, false
}
func (c *Cache) Put(key string, data []byte) model.CacheMeta {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.sweep(now)
	if e := c.items[key]; e != nil {
		c.remove(e)
	}
	size := len(key) + len(data)
	if len(data) > c.maxItem || size > c.maxBytes {
		c.Skipped++
		return model.CacheMeta{}
	}
	for len(c.items) >= c.maxEntries || c.bytes+size > c.maxBytes {
		c.remove(c.lru.Back())
		c.Evicted++
	}
	v := entry{key, append([]byte(nil), data...), now, now.Add(c.ttl)}
	c.items[key] = c.lru.PushFront(v)
	c.bytes += size
	return c.meta(v, false)
}
func (c *Cache) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.mu.Lock()
			c.sweep(now)
			c.mu.Unlock()
		}
	}
}
func (c *Cache) Stats() map[string]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]uint64{"cache_entries": uint64(len(c.items)), "cache_bytes": uint64(c.bytes), "cache_hits_total": c.Hits, "cache_misses_total": c.Misses, "cache_expired_total": c.Expired, "cache_evictions_total": c.Evicted, "cache_skipped_total": c.Skipped}
}
