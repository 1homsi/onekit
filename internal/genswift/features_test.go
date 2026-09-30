package genswift

import "testing"

const featureSchema = `
package app
/// Visibility.
enum Visibility { PRIVATE PUBLIC_VIEW }
message Meta { owner: string @email }
message Note {
  /// The id.
  id: string @uuid
  title: string @len(1, 20)
  count: int32 @range(0, 100)
  big: int64
  huge: uint64
  ratio: float64 @gte(0)
  tags: string[] @max_items(3)
  data: bytes
  hex: bytes @encode("hex")
  at: timestamp
  day: timestamp @encode("date")
  secs: timestamp @encode("unix_seconds")
  vis: Visibility
  vis_num: Visibility @encode("number")
  maybe: string?
  meta: Meta
  flat: Meta @flatten(prefix: "m_")
  labels: map[string, int64]
  raw_json: json
  old: string @deprecated("use title")
  body: oneof(discriminator: "kind") {
    text: string @tag("text")
    meta_ref: Meta @tag("meta")
    n: int64 @tag("n")
  }
}
message Ids { v: int64[] @unwrap }
message GetNote { id: string @uuid limit: int32? @query tags: string[] @query }
message NotFound @status(404) { code: string }
/// Notes API.
service Notes {
  /// Fetch.
  get(GetNote) -> Note | NotFound @get("/notes/{id}")
  create(Note) -> Note @post("/notes")
  ids(GetNote) -> Ids @get("/ids/{id}")
  watch(GetNote) -> Note | NotFound @get("/watch/{id}") @stream
}
`

const featureMain = `import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

func check(_ ok: Bool, _ what: @autoclosure () -> String) {
    if !ok {
        FileHandle.standardError.write(Data((what() + "\n").utf8))
        exit(1)
    }
}

func bodyData(_ request: URLRequest) -> Data {
    if let body = request.httpBody { return body }
    guard let stream = request.httpBodyStream else { return Data() }
    stream.open()
    defer { stream.close() }
    var out = Data()
    var buffer = [UInt8](repeating: 0, count: 4096)
    while stream.hasBytesAvailable {
        let count = stream.read(&buffer, maxLength: buffer.count)
        if count <= 0 { break }
        out.append(buffer, count: count)
    }
    return out
}

final class Stub: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let url = request.url!
        var status = 200
        var headers: [String: String] = [:]
        var body = Data()
        let path = url.path
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        if path == "/notes/0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f" {
            check(query.filter { $0.name == "tags" }.map { $0.value ?? "" }.joined(separator: ",") == "a,b", "repeated query: \(query)")
            check(query.first { $0.name == "limit" }?.value == "5", "optional query")
            check(request.value(forHTTPHeaderField: "x-trace") == "t1" && request.value(forHTTPHeaderField: "x-base") == "b", "headers")
            body = Data("{\"title\":\"found\",\"big\":\"7\"}".utf8)
        } else if path.hasPrefix("/notes/") {
            status = 404
            body = Data("{\"code\":\"missing\"}".utf8)
        } else if path == "/notes" && request.httpMethod == "POST" {
            body = bodyData(request)
        } else if path.hasPrefix("/watch/") {
            headers["Content-Type"] = "text/event-stream"
            body = Data("data: {\"title\":\"one\"}\r\n\r\n: comment\n\ndata: {\"title\":\ndata: \"two\"}\n\nevent: error\ndata: {\"message\":\"boom\"}\n\n".utf8)
        } else {
            status = 500
            body = Data("plain failure".utf8)
        }
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

let note = Note(
    id: "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f",
    title: "hi",
    big: -9007199254740993,
    huge: 18446744073709551615,
    tags: ["a"],
    data: Data([1, 2, 3]),
    hex: Data([255, 1]),
    at: Date(timeIntervalSince1970: 1767323045.000006),
    day: Date(timeIntervalSince1970: 1767312000),
    secs: Date(timeIntervalSince1970: 1767323045),
    vis: .publicView,
    visNum: .publicView,
    meta: Meta(owner: "a@b.io"),
    flat: Meta(owner: "c@d.io"),
    labels: ["x": 5],
    rawJson: ["free": [1, true] as [Any]],
    body: .n(42)
)
let wire = note.toJSONValue()
let expected: [String: Any] = [
    "id": "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f",
    "title": "hi",
    "big": "-9007199254740993",
    "huge": "18446744073709551615",
    "tags": ["a"],
    "data": "AQID",
    "hex": "ff01",
    "at": "2026-01-02T03:04:05.000006Z",
    "day": "2026-01-02",
    "secs": 1767323045,
    "vis": "PUBLIC_VIEW",
    "vis_num": 1,
    "meta": ["owner": "a@b.io"],
    "m_owner": "c@d.io",
    "labels": ["x": 5],
    "raw_json": ["free": [1, true]],
    "body": ["kind": "n", "n": "42"],
]
check(onekitJSONEquals(wire, expected), "wire mismatch: \(wire)")
let roundTrip = try Note(json: JSONSerialization.jsonObject(with: onekitJSONData(wire)))
check(roundTrip == note, "round trip: \(roundTrip)")
check(roundTrip.body == .n(42), "oneof decode")
check(try Note(json: ["body": ["kind": "meta", "meta_ref": ["owner": "x@y.io"]]]).body == .metaRef(Meta(owner: "x@y.io")), "message variant")
check(try Note(json: ["body": ["kind": "nope"]]).body == nil, "unknown variant")
let nanos = try Note(json: ["at": "2026-01-02T03:04:05.123456789Z"]).at!
check(abs(nanos.timeIntervalSince1970 - 1767323045.123456) < 1e-5, "fractional seconds: \(nanos.timeIntervalSince1970)")
let offset = try Note(json: ["at": "2026-01-02T05:04:05+02:00"]).at!
check(offset == Date(timeIntervalSince1970: 1767323045), "offset timestamp")
check(note.validate().isEmpty, "valid note: \(note.validate())")
let bad = Note(id: "nope", title: "", count: 101, ratio: -1, tags: ["a", "b", "c", "d"], meta: Meta(owner: "bad"))
let violations = bad.validate()
for want in ["id must be a valid UUID", "title has invalid length", "count violates @range", "ratio violates @gte", "tags must contain at most 3 items", "meta: owner must be a valid email"] {
    check(violations.contains(want), "missing \(want) in \(violations)")
}
check(onekitJSONEquals(Ids(v: [1, -2]).toJSONValue(), ["1", "-2"]), "unwrap strings")
check(try Ids(json: ["3", 4]).v == [3, 4], "unwrap decode")
check(try Visibility.fromWire("PRIVATE") == .private_, "enum wire")
check(Visibility.publicView.ordinal == 1, "enum ordinal")

let configuration = URLSessionConfiguration.ephemeral
configuration.protocolClasses = [Stub.self]
let session = URLSession(configuration: configuration)
let client = NotesClient("http://stub.local/", session: session, headers: ["x-base": "b", "x-trace": "base"])
let got = try await client.get(GetNote(id: "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f", limit: 5, tags: ["a", "b"]), headers: ["x-trace": "t1"])
check(got.title == "found" && got.big == 7, "get: \(got)")
do {
    _ = try await client.get(GetNote(id: "1f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f"))
    check(false, "expected NotFound")
} catch let error as NotFound {
    check(error.code == "missing", "typed error")
}
do {
    _ = try await client.get(GetNote(id: "bad"))
    check(false, "expected validation error")
} catch OnekitError.validation(let problems) {
    check(problems.contains("id must be a valid UUID"), "validation: \(problems)")
}
let echoed = try await client.create(note)
check(echoed == note, "post round trip: \(echoed)")
var events: [String] = []
do {
    for try await event in client.watch(GetNote(id: "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f")) {
        events.append(event.title)
    }
    check(false, "expected stream error")
} catch OnekitError.stream(let payload) {
    check((payload as? [String: Any])?["message"] as? String == "boom", "stream error payload")
}
check(events.joined(separator: ",") == "one,two", "sse events: \(events)")
do {
    _ = try await client.ids(GetNote(id: "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f"))
    check(false, "expected unexpected status")
} catch OnekitError.unexpectedStatus(let status, let body, _) {
    check(status == 500 && String(decoding: body, as: UTF8.self) == "plain failure", "unexpected status")
}
print("OK")
`

func TestSwiftClientCoversEveryFieldKind(t *testing.T) {
	runSwiftSchema(t, featureSchema, featureMain)
}
