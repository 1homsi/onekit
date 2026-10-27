package genpy

import "testing"

func TestPythonErrorClassDefaultsAndEquality(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Detail {
  field: string;
}

@error
model InvalidError {
  @statusCode _: 400;
  message: string;
  details: Detail[];
  meta: Record<string>;
}
`, `
from models import InvalidError

error = InvalidError()
assert error.details == [] and error.meta == {} and error.message == "", vars(error)
assert InvalidError(message="x") == InvalidError.from_dict({"message": "x"})
assert InvalidError(message="x") != InvalidError(message="y")
print("OK")
`)
}
