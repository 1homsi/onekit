package genpy

import "testing"

func TestPythonRootUnwrapInt64UsesStrings(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Ids {
  @Onkt.unwrap
  @Onkt.encode("string")
  v: int64[];
}

model Total {
  @Onkt.unwrap
  @encode(string)
  v: uint64;
}
`, `
from models import Ids, Total
assert Ids(v=[1, -2]).to_dict() == ["1", "-2"]
assert Ids.from_dict(["1", "-2"]).v == [1, -2]
assert Total(v=18446744073709551615).to_dict() == "18446744073709551615"
assert Total.from_dict("7").v == 7
print("OK")
`)
}
