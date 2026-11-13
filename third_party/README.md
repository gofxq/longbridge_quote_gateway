# Longbridge SDK compatibility patch

`openapi-go/` is the official github.com/longbridge/openapi-go v0.30.1
source, with its original licenses. A local `replace` keeps builds reproducible.

One change in `quote/types.go`, function `doRatio`: assign the converted
turnover rate to `TurnoverRate`, rather than overwriting `Turnover` (traded
value). Without this correction the SDK returns an incorrect monetary value
from CalcIndex. The gateway regression test exercises a real SDK HTTP response
through this conversion. Remove the replacement when an upstream release fixes
this defect and passes the regression test. No trading context is instantiated.
