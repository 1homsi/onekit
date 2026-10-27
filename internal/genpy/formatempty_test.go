package genpy

import "testing"

func TestPythonFormatValidatorsSkipEmptyStrings(t *testing.T) {
	runPythonSchema(t, `namespace app;

model M {
  @format("email")
  email: string;
  @format("uuid")
  id: string;
  @format("uri")
  site: string;
  @pattern("^[A-Z]+$")
  code: string;
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
