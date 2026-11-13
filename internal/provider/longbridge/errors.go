package longbridge

import (
	"errors"

	protocol "github.com/longbridge/openapi-protocol/go"

	"github.com/gofxq/longbridge_quote_gateway/internal/model"
)

// Codes are interpreted with endpoint context: 301600 is ambiguous across APIs.
// https://open.longbridge.com/docs/quote/pull/intraday
// https://open.longbridge.com/docs/quote/pull/history-candlestick
func mapSDKError(err error) *model.Error {
	var e *protocol.LBError
	if errors.As(err, &e) {
		switch e.Code {
		case 301604:
			return model.E("MARKET_DATA_PERMISSION_DENIED", 403)
		case 301606:
			return model.E("UPSTREAM_UNAVAILABLE", 503)
		}
	}
	return model.E("UPSTREAM_ERROR", 502)
}
func endpointError(err error, kind string) *model.Error {
	var e *protocol.LBError
	if errors.As(err, &e) {
		if e.Code == 301600 && e.Status == 7 && (kind == "intraday" || kind == "depth" || kind == "trades") {
			return model.E("SYMBOL_NOT_FOUND", 404)
		}
		if e.Code == 301607 && kind == "history" {
			return model.E("MARKET_DATA_PERMISSION_DENIED", 403)
		}
	}
	return mapError(err)
}
