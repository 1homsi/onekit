package genrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const rustTypesFixture = `
package fixture

message Address {
  street: string
  city: string
}

message EmailAuth {
  email: string @email
}

message TokenAuth {
  token: string @len(10, 100)
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message Tags {
  values: string[] @unwrap
}

message Envelope {
  id: int64
  payload: bytes @encode("hex")
  optional_payload: bytes?
  chunks: bytes[]
  blob_map: map[string, bytes]
  callback_url: string @uri
  status: Status
  numeric_status: Status @encode("number")
  tags: Tags
  home: Address @flatten(prefix: "home_")
  auth_method: oneof(discriminator: "auth_type") {
    email: EmailAuth
    token: TokenAuth
  }
}
`

const rustTypesCargoToml = `
[package]
name = "onekit-rust-types-fixture"
version = "0.1.0"
edition = "2024"

[dependencies]
base64 = "0.22"
regex = "1"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
url = "2"
uuid = "1"
validator = "0.20"
`

const rustTypesHarness = `

#[cfg(test)]
mod generated_tests {
    use super::*;

    #[test]
    fn wire_mapping_and_validation_round_trip() {
        let blob_map = std::collections::HashMap::from([(String::from("primary"), vec![b'h', b'i'])]);
        let value = Envelope {
            id: 42,
            payload: vec![0, 255],
            optional_payload: Some(vec![1, 2, 3]),
            chunks: vec![vec![4, 5], vec![6]],
            blob_map,
            callback_url: "https://example.com/callback".into(),
            status: Status::Active,
            numeric_status: Status::Active,
            tags: Some(Box::new(Tags { values: vec!["rust".into(), "api".into()] })),
            home: Some(Box::new(Address { street: "Main".into(), city: "Beirut".into() })),
            auth_method: Some(EnvelopeAuthMethod::Email(EmailAuth { email: "ada@example.com".into() })),
        };
        value.validate().unwrap();

        let wire = serde_json::to_value(&value).unwrap();
        assert_eq!(wire["id"], "42");
        assert_eq!(wire["payload"], "00ff");
        assert_eq!(wire["optional_payload"], "AQID");
        assert_eq!(wire["chunks"], serde_json::json!(["BAU=", "Bg=="]));
        assert_eq!(wire["blob_map"], serde_json::json!({"primary": "aGk="}));
        assert_eq!(wire["status"], "active");
        assert_eq!(wire["numeric_status"], 1);
        assert_eq!(wire["tags"], serde_json::json!(["rust", "api"]));
        assert_eq!(wire["home_street"], "Main");
        assert_eq!(wire["auth_method"]["auth_type"], "email");
        assert_eq!(wire["auth_method"]["email"]["email"], "ada@example.com");

        let decoded: Envelope = serde_json::from_value(wire).unwrap();
        assert_eq!(decoded, value);
    }
}
`

func TestGeneratedRustTypesCompileAndRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}
	ast, err := onklang.Parse(rustTypesFixture)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "fixture.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	var file *onkir.File
	if len(pkg.Files) > 0 {
		file = pkg.Files[0]
	}
	if file == nil {
		t.Fatal("compiled fixture did not contain a file")
	}

	dir := t.TempDir()
	if mkdirErr := os.MkdirAll(filepath.Join(dir, "src"), 0o755); mkdirErr != nil {
		t.Fatalf("mkdir fixture: %v", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(rustTypesCargoToml), 0o644); writeErr != nil {
		t.Fatalf("write Cargo.toml: %v", writeErr)
	}
	source := append(GenerateTypes(file), rustTypesHarness...)
	if writeErr := os.WriteFile(filepath.Join(dir, "src", "lib.rs"), source, 0o644); writeErr != nil {
		t.Fatalf("write generated lib.rs: %v", writeErr)
	}

	cmd := exec.Command("cargo", "test", "--quiet")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Rust types failed: %v\n%s\nGenerated source:\n%s", err, out, source)
	}
}

func TestGenerateRustClientEncodesCustomBodyFields(t *testing.T) {
	ast, err := onklang.Parse(`
package bodyfixture

enum State {
  UNKNOWN
  READY
}

message AmountRequest { amount: int64 }
message PayloadRequest { payload: bytes @encode(hex) }
message StateRequest { state: State @encode(number) }
message Ack { ok: bool }

service BodyService {
  sendAmount(AmountRequest) -> Ack @post("/amount") @body("amount")
  sendPayload(PayloadRequest) -> Ack @post("/payload") @body("payload")
  sendState(StateRequest) -> Ack @post("/state") @body("state")
}
`)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "body.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	text := string(GenerateClient(pkg.Files[0]))
	for _, want := range []string{
		`serde_json::Value::String((*&req.amount).to_string())`,
		`serde_json::Value::String((&req.payload).iter().map(|byte| format!("{byte:02x}")).collect::<String>())`,
		`serde_json::Value::Number((match &req.state`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated Rust body client missing %q:\n%s", want, text)
		}
	}
}
