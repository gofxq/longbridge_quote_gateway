package longbridge

import (
	"log/slog"

	"github.com/longbridge/openapi-go/config"
	sdkhttp "github.com/longbridge/openapi-go/http"
	"github.com/longbridge/openapi-go/longbridge"
	"github.com/longbridge/openapi-go/quote"
)

// Keep upstream diagnostic arguments out of logs: they can contain credentials.
// Connection state is reported through readiness and these fixed event messages.
type sdkLogger struct{}

func (sdkLogger) SetLevel(string)               {}
func (sdkLogger) Info(string)                   {}
func (sdkLogger) Warn(string)                   { slog.Warn("upstream SDK warning") }
func (sdkLogger) Error(string)                  { slog.Error("upstream SDK error") }
func (sdkLogger) Debug(string)                  {}
func (sdkLogger) Infof(string, ...interface{})  {}
func (sdkLogger) Warnf(string, ...interface{})  { slog.Warn("upstream SDK warning") }
func (sdkLogger) Errorf(string, ...interface{}) { slog.Error("upstream SDK error") }
func (sdkLogger) Debugf(string, ...interface{}) {}
func newQuoteContext(cfg *config.Config, reconnected func(bool)) (*quote.QuoteContext, error) {
	h, err := sdkhttp.NewFromCfg(cfg)
	if err != nil {
		return nil, err
	}
	return quote.New(quote.WithQuoteURL(cfg.QuoteUrl), quote.WithHttpClient(h), quote.WithLogger(sdkLogger{}), quote.WithLogLevel("error"), quote.WithLanguage(cfg.Language), quote.WithEnableOvernight(cfg.EnableOvernight), quote.WithLbOptions(longbridge.NewOptions(longbridge.WithAuthTimeout(cfg.AuthTimeout), longbridge.WithTimeout(cfg.Timeout))), quote.OnReconnect(reconnected))
}
