package quote

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestGatewayTurnoverNotOverwritten(t *testing.T) {
	turnover := decimal.RequireFromString("1234.50")
	rate := decimal.RequireFromString("0.25")
	v := &SecurityCalcIndex{Turnover: &turnover, TurnoverRate: &rate}
	doRatio(v)
	if !v.Turnover.Equal(turnover) {
		t.Fatal("turnover overwritten")
	}
	if v.TurnoverRate.String() != "0.0025" {
		t.Fatal(v.TurnoverRate)
	}
}
