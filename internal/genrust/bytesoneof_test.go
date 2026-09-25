package genrust

import "testing"

const rustBytesOneofFixture = `
package app
message Blob {
  content: oneof {
    raw: bytes @tag("raw")
    text: string @tag("text")
  }
}
service Store { put(Blob) -> Blob @post("/blobs") }
`

const rustBytesOneofMain = `mod generated;
use generated::types::{Blob, BlobContent};

fn main() {
    let blob = Blob { content: Some(BlobContent::Raw(vec![1, 2, 3])) };
    let json = serde_json::to_string(&blob).unwrap();
    assert_eq!(json, r#"{"content":{"raw":"AQID","type":"raw"}}"#);
    let back: Blob = serde_json::from_str(&json).unwrap();
    assert_eq!(back, blob);
    assert!(serde_json::from_str::<Blob>(r#"{"content":{"type":"raw","raw":[1,2,3]}}"#).is_err());
    println!("OK");
}
`

func TestGeneratedRustEncodesBytesOneofVariantsAsBase64(t *testing.T) {
	runRustWSCrate(t, rustBytesOneofFixture, "onekit-rust-bytes-oneof", rustBytesOneofMain, true)
}
