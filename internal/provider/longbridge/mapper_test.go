package longbridge

import (
	"fmt"
	"testing"

	"github.com/longbridge/openapi-go/quote"
	protocol "github.com/longbridge/openapi-protocol/go"
	"github.com/shopspring/decimal"
)

func TestDecimalDerivation(t *testing.T) {
	for _, tc := range []struct{ last, prev, change, pct string }{{"268.50", "266.20", "2.30", "0.86"}, {"0.99995", "1.00000", "-0.00005", "-0.01"}, {"1.00005", "1.00000", "0.00005", "0.01"}, {"1.00", "0.00", "1.00", ""}} {
		last := decimal.RequireFromString(tc.last)
		prev := decimal.RequireFromString(tc.prev)
		v := derive(&quote.SecurityQuote{LastDone: &last, PrevClose: &prev})
		if *v.Change != tc.change {
			t.Fatal(v)
		}
		if tc.pct == "" {
			if v.ChangePercent != nil {
				t.Fatal("zero divisor")
			}
		} else if *v.ChangePercent != tc.pct {
			t.Fatal(*v.ChangePercent)
		}
	}
	if derive(&quote.SecurityQuote{}).Change != nil {
		t.Fatal("missing input")
	}
}
func TestTypedErrorMapping(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &protocol.LBError{Status: 7, Code: 301600})
	if endpointError(err, "history").Status == 404 {
		t.Fatal("ambiguous error mapped to missing symbol")
	}
	if endpointError(err, "intraday").Status != 404 {
		t.Fatal("known error not mapped")
	}
	if mapError(&protocol.LBError{Code: 301604}).Status != 403 {
		t.Fatal("permission")
	}
}
