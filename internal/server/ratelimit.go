package server

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}
type ipLimits struct {
	mu         sync.Mutex
	entries    map[string]ipEntry
	rps, burst int
}

func (l *ipLimits) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if e, ok := l.entries[ip]; ok {
		e.seen = now
		l.entries[ip] = e
		return e.limiter.Allow()
	}
	if len(l.entries) >= 4096 {
		for k, e := range l.entries {
			if now.Sub(e.seen) > 5*time.Minute {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
	}
	e := ipEntry{rate.NewLimiter(rate.Limit(l.rps), l.burst), now}
	l.entries[ip] = e
	return e.limiter.Allow()
}
