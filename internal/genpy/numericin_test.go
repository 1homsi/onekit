package genpy

import "testing"

func TestPythonIntegerInValidation(t *testing.T) {
	runPythonSchema(t, `
package app
message Page {
  size: int32 @in(10, 25, 50)
  version: int64? @in(1, 2)
}
`, `
from models import Page
Page(size=25, version=2).validate()
for bad in (Page(size=30), Page(size=10, version=3)):
    try:
        bad.validate()
        raise SystemExit("accepted " + repr(bad))
    except ValueError as error:
        assert "must be one of the allowed values" in str(error), error
print("OK")
`)
}
