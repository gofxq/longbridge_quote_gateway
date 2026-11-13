// Package model defines the gateway contract independently of the upstream SDK.
package model

import (
	"encoding/json"
	"time"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"http_status"`
	Symbol  string `json:"symbol,omitempty"`
}

func (e *Error) Error() string { return e.Code }
func E(code string, status int) *Error {
	return &Error{Code: code, Message: messages[code], Status: status}
}

var messages = map[string]string{"INVALID_PARAMETER": "invalid query parameters", "UNAUTHORIZED": "valid bearer token required", "RATE_LIMITED": "request rate exceeded", "REQUEST_CAPACITY_EXCEEDED": "request capacity exceeded", "UPSTREAM_QUEUE_FULL": "upstream capacity exceeded", "UPSTREAM_QUEUE_TIMEOUT": "upstream scheduling timed out", "UPSTREAM_TIMEOUT": "upstream request timed out", "REQUEST_TIMEOUT": "request timed out", "UPSTREAM_UNAVAILABLE": "upstream unavailable", "UPSTREAM_ERROR": "upstream request failed", "SYMBOL_DATA_MISSING": "upstream omitted requested symbol", "SYMBOL_NOT_FOUND": "symbol does not exist", "MARKET_DATA_PERMISSION_DENIED": "market data permission denied"}

type Query struct {
	Kind    string
	Symbol  string
	Period  string
	Count   int
	Adjust  string
	Start   string
	End     string
	Market  string
	Indexes string
}

func (q Query) Key() string   { b, _ := json.Marshal(q); return string(b) }
func (q Query) Group() string { q.Symbol = ""; return q.Key() }
func (q Query) Batchable() bool {
	return q.Kind == "quotes" || q.Kind == "securities" || q.Kind == "calc-indexes"
}

type CacheMeta struct {
	Hit       bool       `json:"cache_hit"`
	Stored    bool       `json:"stored"`
	CachedAt  *time.Time `json:"cached_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	TTL       float64    `json:"ttl_seconds"`
}
type Result struct {
	Data json.RawMessage
	Meta CacheMeta
	Err  *Error
}
type Envelope struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Data      any            `json:"data"`
	Errors    []Error        `json:"errors"`
	RequestID string         `json:"request_id"`
	Meta      map[string]any `json:"meta"`
}
