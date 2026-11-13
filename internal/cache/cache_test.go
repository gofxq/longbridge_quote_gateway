package cache

import (
	"testing"
	"time"
)

func TestBudgetsLRUImmutableAndTTL(t *testing.T) {
	c := New(25*time.Millisecond, 2, 12, 8)
	c.Put("a", []byte("1234"))
	c.Put("b", []byte("5678"))
	r, _ := c.Get("a")
	r.Data[0] = 'x'
	c.Put("c", []byte("abcd"))
	if _, ok := c.Get("b"); ok {
		t.Fatal("LRU entry retained")
	}
	r, _ = c.Get("a")
	if string(r.Data) != "1234" {
		t.Fatal("mutable cache")
	}
	if c.Stats()["cache_bytes"] > 12 {
		t.Fatal("budget")
	}
	if c.Put("huge", []byte("123456789")).Stored {
		t.Fatal("oversized cached")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry read")
	}
	c.Put("d", []byte("123"))
	if c.Stats()["cache_entries"] != 1 {
		t.Fatal("expired entries not swept")
	}
}
func TestReplacementAccounting(t *testing.T) {
	c := New(time.Minute, 3, 20, 15)
	c.Put("a", []byte("12345"))
	c.Put("a", []byte("1"))
	if c.Stats()["cache_bytes"] != 2 {
		t.Fatal(c.Stats())
	}
}
