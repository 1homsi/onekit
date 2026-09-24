package genpy

import "testing"

func TestPythonFormatValidatorsSkipEmptyStrings(t *testing.T) {
	runPythonSchema(t, `
package app
message M {
  email: string @email
  id: string @uuid
  site: string @uri
  code: string @pattern("^[A-Z]+$")
}
`, `
from models import M

M().validate()
try:
    M(email="nope", id="x", site="y", code="z").validate()
    raise SystemExit("expected validation error")
except ValueError as error:
    for field in ("email", "id", "site", "code"):
        assert field in str(error), error
print("OK")
`)
}
