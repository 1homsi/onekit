package genswift

import "testing"

const edgeSchema = `namespace app;

enum Kind {
  NEW,
  DEFAULT,
  VALUES,
}

model Empty {}

model Inner {
  note: string;
}

model Outer {
  child: Child;
  mode: Mode;
}

@Onkt.parent(Outer)
model Child {
  label: string;
}

@Onkt.parent(Outer)
enum Mode {
  FAST,
  SLOW,
}

model Node {
  name: string;
  children: Node[];
  parent?: Node;
}

@discriminated(#{ discriminatorPropertyName: "type", envelope: "none" })
union AwkwardPick {
  @Onkt.tag("inner") inner: Inner,
}

@Onkt.discriminator("t")
union AwkwardAlt {
  @Onkt.tag("blob") blob: bytes,
  @Onkt.tag("kind") kind_ref: Kind,
  @Onkt.tag("empty") empty: Empty,
}

model Awkward {
  switch: string;
  var: int32;
  values: string[];
  kind: Kind;
  maybe_kind?: Kind;
  maybe_bytes?: bytes;
  @encode(string)
  maybe_big?: int64;
  @encode(string)
  maybe_huge?: uint64;
  num_big: int64;
  @Onkt.encode("unix_millis")
  millis: utcDateTime;
  @Onkt.encode("base64url_raw")
  url_bytes: bytes;
  @Onkt.empty("null")
  null_empty: Inner;
  @Onkt.empty("omit")
  omit_empty: Inner;
  @Onkt.values(10, 25)
  size: int32;
  @Onkt.values(1, 2)
  @encode(string)
  huge_in?: uint64;
  description: string;
  json: string;
  pick: AwkwardPick;
  @Onkt.required
  alt: AwkwardAlt;
  @Onkt.required
  must?: string;
  @Onkt.required
  must_list: string[];
  @Onkt.required
  must_inner: Inner;
  counts: Record<uint64>;
  levels: Kind[];
  stamps: utcDateTime[];
}

model Ids {
  @Onkt.unwrap
  @Onkt.encode("string")
  v: int64[];
}

model Blob {
  @Onkt.unwrap
  data: unknown;
}

model Bag {
  items: unknown[];
  extra: Record<unknown>;
}

model Lookup {
  @Onkt.unwrap
  v: Record<Inner>;
}

model Wrapped {
  @Onkt.unwrap
  v: Inner;
}

@error
model Problem {
  @statusCode _: 409;
  reasons: string[];
  details: Record<string>;
}

interface Weird {
  @post
  @route("/close")
  close(...Empty): Empty;

  @post
  @route("/get")
  get(...Awkward): Awkward | Problem;
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
check(onekitJSONEquals(Blob(data: ["k": 1] as [String: Any]).toJSONValue(), ["k": 1]), "unwrap json")
check(try Blob(json: ["k": 1]).data != nil && Blob(json: NSNull()).data == nil, "unwrap json decode")
let bag = try Bag(json: ["items": [1, "a", ["x": true]], "extra": ["k": [1, 2]]])
check(bag.items.count == 3 && bag.extra["k"] != nil, "json collections decode")
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
