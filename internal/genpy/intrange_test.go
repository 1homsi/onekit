package genpy

import "testing"

func TestPythonValidateChecksIntegerRanges(t *testing.T) {
	runPythonSchema(t, `
package app
message Counts {
  small: int32
  count: uint32
  big: int64?
}
`, `
from models import Counts

Counts(small=5, count=0, big=2**62).validate()
for bad in (Counts(small=2**31), Counts(count=-1), Counts(small=True), Counts(small="5"), Counts(big=2**63)):
    try:
        bad.validate()
        raise SystemExit("accepted out-of-range value: %r" % bad)
    except ValueError:
        pass
print("OK")
`)
}
