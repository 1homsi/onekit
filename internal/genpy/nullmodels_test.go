package genpy

import "testing"

func TestPythonModelsTolerateNullAndRejectNonFinite(t *testing.T) {
	runPythonSchema(t, `namespace app;

model Probe {
  f: float64;
  arr: string[];
  m: Record<string>;
  @encode(string)
  big: int64;
  s: string;
  n: int32;
  tags: Probe[];
  @pattern("^[a-z]+$")
  pat: string;
}

interface Echo {
  @post
  @route("/echo")
  echo(...Probe): Probe;
}
`, `
from models import Probe
from client import EchoClient

p = Probe.from_dict({"arr": None, "m": None, "big": None, "s": None, "n": None, "f": None, "tags": None})
assert p.arr == [] and p.m == {} and p.big == 0 and p.s == "" and p.n == 0 and p.f == 0.0 and p.tags == [], p
assert Probe.from_dict({"arr": ["a"], "big": "7", "s": "x"}).big == 7

client = EchoClient("http://127.0.0.1:1")
for bad in (float("inf"), float("-inf"), float("nan")):
    try:
        client.echo(Probe(f=bad))
        raise SystemExit("%r must not be sent as a bare JSON token" % bad)
    except ValueError as error:
        assert "JSON compliant" in str(error) or "finite" in str(error), error

try:
    Probe(pat="abc\n").validate()
    raise SystemExit("a trailing newline must not satisfy $")
except ValueError as error:
    assert "pat has invalid format" in str(error), error
Probe(pat="abc").validate()
print("OK")
`)
}

func TestPyEndAnchorsRewritesOnlyRealAnchors(t *testing.T) {
	cases := map[string]string{
		`^[a-z]+$`:   `^[a-z]+\Z`,
		`a\$b$`:      `a\$b\Z`,
		`[$]x`:       `[$]x`,
		`(a|b)$`:     `(a|b)\Z`,
		`no anchors`: `no anchors`,
	}
	for in, want := range cases {
		if got := pyEndAnchors(in); got != want {
			t.Errorf("pyEndAnchors(%q) = %q, want %q", in, got, want)
		}
	}
}
