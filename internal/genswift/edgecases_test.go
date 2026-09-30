package genswift

import "testing"

const edgeSchema = `
package app
enum Kind { NEW DEFAULT VALUES }
message Empty {}
message Inner { note: string }
message Outer {
  message Child { label: string }
  enum Mode { FAST SLOW }
  child: Child
  mode: Mode
}
message Node { name: string children: Node[] parent: Node? }
message Awkward {
  switch: string
  var: int32
  values: string[]
  kind: Kind
  maybe_kind: Kind?
  maybe_bytes: bytes?
  maybe_big: int64?
  maybe_huge: uint64?
  num_big: int64 @encode("number")
  millis: timestamp @encode("unix_millis")
  url_bytes: bytes @encode("base64url_raw")
  null_empty: Inner @empty("null")
  omit_empty: Inner @empty("omit")
  size: int32 @in(10, 25)
  huge_in: uint64? @in(1, 2)
  description: string
  json: string
  pick: oneof(flatten: true) {
    inner: Inner @tag("inner")
  }
  alt: oneof(discriminator: "t") {
    blob: bytes @tag("blob")
    kind_ref: Kind @tag("kind")
    empty: Empty @tag("empty")
  } @required
  must: string? @required
  must_list: string[] @required
  must_inner: Inner @required
  counts: map[string, uint64]
  levels: Kind[]
  stamps: timestamp[]
}
message Ids { v: int64[] @unwrap }
message Lookup { v: map[string, Inner] @unwrap }
message Wrapped { v: Inner @unwrap }
message Problem @status(409) { reasons: string[] details: map[string, string] }
service Weird {
  close(Empty) -> Empty @post("/close")
  get(Awkward) -> Awkward | Problem @post("/get")
}
`

const edgeMain = `import Foundation

func check(_ ok: Bool, _ what: @autoclosure () -> String) {
    if !ok {
        FileHandle.standardError.write(Data((what() + "\n").utf8))
        exit(1)
    }
}

check(Kind.new.rawValue == "NEW" && Kind.default_.rawValue == "DEFAULT" && Kind.values.rawValue == "VALUES", "enum names")
check(OuterMode.fast.rawValue == "FAST", "nested enum")
let outer = Outer(child: OuterChild(label: "x"), mode: .slow)
check(onekitJSONEquals(outer.toJSONValue(), ["child": ["label": "x"], "mode": "SLOW"]), "nested: \(outer)")
check(try Outer(json: outer.toJSONValue()) == outer, "nested round trip")
check(onekitJSONEquals(Empty().toJSONValue(), [String: Any]()), "empty")

let leaf = Node(name: "leaf")
let root = Node(name: "root", children: [leaf], parent: Node(name: "up"))
check(try Node(json: root.toJSONValue()) == root, "recursive message round trip")
check(root.parent?.name == "up" && root.children[0].name == "leaf", "recursive access")

let a = Awkward(
    switch_: "x",
    var_: 3,
    values: ["v"],
    kind: .default_,
    maybeKind: .values,
    maybeBytes: Data(),
    maybeBig: 0,
    maybeHuge: 7,
    numBig: 9,
    millis: Date(timeIntervalSince1970: 1.5),
    urlBytes: Data([251, 255]),
    nullEmpty: Inner(),
    omitEmpty: Inner(),
    size: 25,
    hugeIn: 2,
    description_: "d",
    json_: "j",
    pick: .inner(Inner(note: "flat")),
    alt: .blob(Data([1])),
    must: "yes",
    mustList: ["y"],
    mustInner: Inner(note: "n"),
    counts: ["big": 18446744073709551615],
    levels: [.new, .values],
    stamps: [Date(timeIntervalSince1970: 0)]
)
let wire = a.toJSONValue()
let expected: [String: Any] = [
    "switch": "x",
    "var": 3,
    "values": ["v"],
    "kind": "DEFAULT",
    "maybe_kind": "VALUES",
    "maybe_bytes": "",
    "maybe_big": "0",
    "maybe_huge": "7",
    "num_big": 9,
    "millis": 1500,
    "url_bytes": "-_8",
    "null_empty": NSNull(),
    "size": 25,
    "huge_in": "2",
    "description": "d",
    "json": "j",
    "pick": ["note": "flat", "type": "inner"],
    "alt": ["t": "blob", "blob": "AQ=="],
    "must": "yes",
    "must_list": ["y"],
    "must_inner": ["note": "n"],
    "counts": ["big": UInt64.max],
    "levels": ["NEW", "VALUES"],
    "stamps": ["1970-01-01T00:00:00Z"],
]
check(onekitJSONEquals(wire, expected), "wire: \(wire)")
check(try Awkward(json: JSONSerialization.jsonObject(with: onekitJSONData(wire))) == a, "round trip")
check(a.validate().isEmpty, "valid: \(a.validate())")
check(try Awkward(json: ["alt": ["t": "kind", "kind": "NEW"]]).alt == .kindRef(.new), "enum variant")
check(try Awkward(json: ["alt": ["t": "empty", "empty": [String: Any]()]]).alt == .empty(Empty()), "empty variant")

let violations = Awkward(size: 3, hugeIn: 5).validate()
for want in [
    "size must be one of the allowed values",
    "huge_in must be one of the allowed values",
    "alt is required",
    "must is required",
    "must_list is required",
    "must_inner is required",
] {
    check(violations.contains(want), "missing \(want) in \(violations)")
}
check(Awkward(size: 10, alt: .blob(Data()), must: "", mustList: ["a"], mustInner: Inner()).validate().contains("must is required"), "empty required string")

check(onekitJSONEquals(Ids(v: [1]).toJSONValue(), ["1"]), "unwrap list")
check(onekitJSONEquals(Lookup(v: ["k": Inner(note: "z")]).toJSONValue(), ["k": ["note": "z"]]), "unwrap map")
check(try Lookup(json: ["k": ["note": "z"]]).v["k"]?.note == "z", "unwrap map decode")
check(onekitJSONEquals(Wrapped(v: Inner(note: "w")).toJSONValue(), ["note": "w"]), "unwrap message")
check(try Wrapped(json: ["note": "w"]).v?.note == "w", "unwrap message decode")

let problem = try Problem(json: ["reasons": ["a"], "details": ["k": "v"]])
check(problem.reasons == ["a"] && problem.details["k"] == "v", "error class")
let asError: Error = problem
check(asError is Problem, "error conformance")
check(Problem() == Problem(reasons: []), "error equality")
let client = WeirdClient("http://127.0.0.1:1")
check(client.baseURL == "http://127.0.0.1:1", "client base url")
print("OK")
`

func TestSwiftNamingAndUncommonShapes(t *testing.T) {
	runSwiftSchema(t, edgeSchema, edgeMain)
}
