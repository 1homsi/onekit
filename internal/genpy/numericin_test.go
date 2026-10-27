package genpy

import "testing"

func TestPythonIntegerInValidation(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Page {
  @Onkt.values(10, 25, 50)
  size: int32;
  @Onkt.values(1, 2)
  @encode(string)
  version?: int64;
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
