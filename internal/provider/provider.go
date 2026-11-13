package provider

import (
	"context"

	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

// Fetch receives one homogeneous batch. Results are indexed by canonical business key.
// Implementations must honor ctx, including any internal rate-limit waits.
type Provider interface {
	Fetch(context.Context, []model.Query) map[string]model.Result
	Ready() bool
	Close() error
}
