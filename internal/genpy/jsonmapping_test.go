package genpy

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const jsonMappingFixture = `namespace main;

model Money {
  @encode(string)
  amount_cents: int64;
  amount_number: int64;
  @encode(string)
  optional_amount?: int64;
  @Onkt.encode("string")
  amounts: int64[];
}

enum Status {
  UNSPECIFIED,
  ACTIVE: "active",
}

model StatusHolder {
  status: Status;
  @Onkt.encode("number")
  status_num: Status;
  statuses: Status[];
  status_by_name: Record<Status>;
}

model Document {
  data: bytes;
  @Onkt.encode("hex")
  hash: bytes;
  @Onkt.encode("hex")
  optional_hash?: bytes;
  chunks: bytes[];
  by_hash: Record<bytes>;
}

model Event {
  created_at: utcDateTime;
  @encode("unixTimestamp", int64)
  unix_at: utcDateTime;
}

model Address {
  street: string;
  city: string;
}

model Order {
  id: string;
  @Onkt.flatten("billing_")
  billing: Address;
}

model Meta {
  note: string;
}

model ResponseNull {
  @Onkt.empty("null")
  meta: Meta;
}

model IdList {
  @Onkt.unwrap
  ids: string[];
}

model BlobList {
  @Onkt.unwrap
  values: bytes[];
}

model StatusMap {
  @Onkt.unwrap
  values: Record<Status>;
}
`

const jsonMappingHarness = `
import json

from models import Money, StatusHolder, Document, Event, Order, Address, Meta, ResponseNull, IdList, BlobList, StatusMap, Status


def fail(msg):
    print(msg)
    raise SystemExit(1)


def main():
    # int64 default string, @encode(number), repeated string
    money = Money(amount_cents=12345, amount_number=999, optional_amount=77, amounts=[1, 2, 3])
    d = money.to_dict()
    s = json.dumps(d)
    if d.get("amount_cents") != "12345":
        fail("expected amount_cents as string: " + s)
    if d.get("amount_number") != 999:
        fail("expected amount_number as number: " + s)
    if d.get("optional_amount") != "77":
        fail("expected optional_amount as string: " + s)
    if d.get("amounts") != ["1", "2", "3"]:
        fail("expected amounts as string array: " + s)
    money2 = Money.from_dict(json.loads(s))
    if money2.amount_cents != 12345 or money2.amount_number != 999 or money2.optional_amount != 77 or money2.amounts != [1, 2, 3]:
        fail("round trip mismatch: " + repr(money2))

    # enum default string, @encode(number)
    holder = StatusHolder(status=Status.ACTIVE, status_num=Status.ACTIVE, statuses=[Status.ACTIVE], status_by_name={"primary": Status.ACTIVE})
    hd = holder.to_dict()
    if hd.get("status") != "active":
        fail("expected status as string: " + json.dumps(hd))
    if hd.get("status_num") != 1:
        fail("expected status_num as number: " + json.dumps(hd))
    if hd.get("statuses") != ["active"] or hd.get("status_by_name") != {"primary": "active"}:
        fail("expected repeated/map enum strings: " + json.dumps(hd))
    holder2 = StatusHolder.from_dict(json.loads(json.dumps(hd)))
    if holder2.status != Status.ACTIVE or holder2.status_num != Status.ACTIVE or holder2.statuses != [Status.ACTIVE] or holder2.status_by_name != {"primary": Status.ACTIVE}:
        fail("round trip mismatch: " + repr(holder2))

    # bytes default base64, @encode(hex)
    doc = Document(data=b"hi", hash=b"hi", optional_hash=b"hi", chunks=[b"one", b"two"], by_hash={"primary": b"hi"})
    dd = doc.to_dict()
    if dd.get("data") != "aGk=":
        fail("expected base64 data: " + json.dumps(dd))
    if dd.get("hash") != "6869":
        fail("expected hex hash: " + json.dumps(dd))
    if dd.get("optional_hash") != "6869":
        fail("expected optional hex hash: " + json.dumps(dd))
    if dd.get("chunks") != ["b25l", "dHdv"] or dd.get("by_hash") != {"primary": "aGk="}:
        fail("expected repeated/map byte strings: " + json.dumps(dd))
    doc2 = Document.from_dict(json.loads(json.dumps(dd)))
    if doc2.data != b"hi" or doc2.hash != b"hi" or doc2.optional_hash != b"hi" or doc2.chunks != [b"one", b"two"] or doc2.by_hash != {"primary": b"hi"}:
        fail("round trip mismatch: " + repr(doc2))
    if Document.from_dict({}).optional_hash is not None:
        fail("expected missing optional bytes to remain None")

    # timestamp default rfc3339 (str passthrough), @encode(unix_seconds) (int passthrough)
    ev = Event(created_at="2024-01-15T09:30:00Z", unix_at=1705311000)
    ed = ev.to_dict()
    if ed.get("created_at") != "2024-01-15T09:30:00Z":
        fail("expected rfc3339 created_at: " + json.dumps(ed))
    if ed.get("unix_at") != 1705311000:
        fail("expected unix seconds unix_at: " + json.dumps(ed))
    ev2 = Event.from_dict(json.loads(json.dumps(ed)))
    if ev2.created_at != ev.created_at or ev2.unix_at != ev.unix_at:
        fail("round trip mismatch: " + repr(ev2))

    # flatten
    order = Order(id="o1", billing=Address(street="123 Main", city="Springfield"))
    od = order.to_dict()
    if od.get("billing_street") != "123 Main" or od.get("billing_city") != "Springfield":
        fail("expected flattened billing fields: " + json.dumps(od))
    if "billing" in od:
        fail("expected no nested billing object: " + json.dumps(od))
    order2 = Order.from_dict(json.loads(json.dumps(od)))
    if order2.billing is None or order2.billing.street != "123 Main" or order2.billing.city != "Springfield":
        fail("round trip mismatch: " + repr(order2))

    # empty behavior: null
    resp_empty = ResponseNull(meta=Meta())
    red = resp_empty.to_dict()
    if red.get("meta") is not None:
        fail("expected null meta for empty message: " + json.dumps(red))

    resp_full = ResponseNull(meta=Meta(note="hi"))
    rfd = resp_full.to_dict()
    if rfd.get("meta") != {"note": "hi"}:
        fail("expected preserved meta for non-empty message: " + json.dumps(rfd))

    # root unwrap
    lst = IdList(ids=["a", "b", "c"])
    ld = lst.to_dict()
    if ld != ["a", "b", "c"]:
        fail("expected root-unwrapped array: " + json.dumps(ld))
    lst2 = IdList.from_dict(json.loads(json.dumps(ld)))
    if lst2.ids != ["a", "b", "c"]:
        fail("round trip mismatch: " + repr(lst2))

    blobs = BlobList(values=[b"one", b"two"])
    if blobs.to_dict() != ["b25l", "dHdv"] or BlobList.from_dict(blobs.to_dict()).values != [b"one", b"two"]:
        fail("expected root-unwrapped byte encoding")

    statuses = StatusMap(values={"primary": Status.ACTIVE})
    if statuses.to_dict() != {"primary": "active"} or StatusMap.from_dict(statuses.to_dict()).values != {"primary": Status.ACTIVE}:
        fail("expected root-unwrapped enum map encoding")

    print("OK")


if __name__ == "__main__":
    main()
`

func TestJSONMappingAnnotations(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	ast, err := onktest.Parse(jsonMappingFixture)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.tsp", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]
	typesSrc := GenerateTypes(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.py"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "main.py"), jsonMappingHarness)

	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python run failed: %v\n%s", err, out)
	}
	if got := strings.TrimRight(string(out), "\r\n"); got != "OK" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
