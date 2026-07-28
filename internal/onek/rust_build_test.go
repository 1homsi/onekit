package onek

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rustOnekitToml = `
module = "example.com/ignored/by/rust"
route_prefix = "/api"

[generate.rust-client]
out = "./src/generated"

[generate.rust-server]
out = "./src/generated"
`

const rustCargoToml = `
[package]
name = "onekit-rust-fixture"
version = "0.1.0"
edition = "2024"

[dependencies]
async-stream = "0.3"
axum = "0.8"
base64 = "0.22"
futures-util = "0.3"
regex = "1"
reqwest = { version = "0.12", default-features = false, features = ["json", "stream", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
serde_with = "3"
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
tower = { version = "0.5", features = ["util"] }
url = "2"
urlencoding = "2"
uuid = "1"
validator = "0.20"
`

const rustBusinessServiceOnk = `
package hub.business

message GetBusinessRequest {
  id: string
}

message Business {
  id: string
  name: string
  balance: Money
}

message UpdateBusinessRequest {
  id: string
  business: Business
}

service BusinessService {
  getBusiness(GetBusinessRequest) -> Business @get("/businesses/{id}")
  updateBusiness(UpdateBusinessRequest) -> Business @put("/businesses/{id}") @body("business")
  watchBusiness(GetBusinessRequest) -> Business @get("/businesses/{id}/events") @stream
}
`

const rustServerHarness = `
pub mod generated;

#[cfg(test)]
mod generated_tests {
    use super::generated::common::types::Money;
    use super::generated::hub::business::v1::server::{
        business_service_router, BusinessService, BusinessServiceGetBusinessServerError,
        BusinessServiceUpdateBusinessServerError, BusinessServiceWatchBusinessServerError, RequestContext,
    };
    use super::generated::hub::business::v1::types::{Business, GetBusinessRequest, UpdateBusinessRequest};
    use axum::body::{to_bytes, Body};
    use axum::http::{Request, StatusCode};
    use std::sync::Arc;
    use tower::ServiceExt as _;

    struct Service;

    impl BusinessService for Service {
        async fn get_business(
            &self,
            _context: RequestContext,
            req: GetBusinessRequest,
        ) -> Result<Business, BusinessServiceGetBusinessServerError> {
            Ok(Business {
                id: req.id,
                name: "Acme".into(),
                balance: Some(Box::new(Money { amount_cents: 123, currency: "USD".into() })),
            })
        }

        async fn watch_business(
            &self,
            _context: RequestContext,
            req: GetBusinessRequest,
        ) -> Result<
            std::pin::Pin<Box<dyn futures_util::Stream<Item = Result<Business, BusinessServiceWatchBusinessServerError>> + Send>>,
            BusinessServiceWatchBusinessServerError,
        > {
            let business = Business {
                id: req.id,
                name: "Acme".into(),
                balance: Some(Box::new(Money { amount_cents: 123, currency: "USD".into() })),
            };
            Ok(Box::pin(futures_util::stream::iter(vec![Ok(business)])))
        }

        async fn update_business(
            &self,
            _context: RequestContext,
            req: UpdateBusinessRequest,
        ) -> Result<Business, BusinessServiceUpdateBusinessServerError> {
            req.business
                .map(|business| *business)
                .ok_or_else(|| BusinessServiceUpdateBusinessServerError::InvalidRequest("missing business".into()))
        }
    }

    #[tokio::test]
    async fn generated_router_binds_path_and_serializes_cross_module_types() {
        let response = business_service_router(Arc::new(Service))
            .oneshot(
                Request::builder()
                    .uri("/api/hub/business/v1/businesses/acme")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(value["id"], "acme");
        assert_eq!(value["balance"]["amount_cents"], "123");
    }
}
`

func TestBuildGeneratesCompilingRustClientServerAndModules(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo toolchain not available")
	}

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "onekit.toml"), rustOnekitToml)
	writeTestFile(t, filepath.Join(dir, "common", "money.onk"), commonMoneyOnk)
	writeTestFile(t, filepath.Join(dir, "hub", "business", "v1", "service.onk"), rustBusinessServiceOnk)

	if err := Build(dir); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	generated := filepath.Join(dir, "src", "generated")
	for _, rel := range []string{
		"mod.rs",
		filepath.Join("common", "mod.rs"),
		filepath.Join("common", "types.rs"),
		filepath.Join("hub", "business", "v1", "mod.rs"),
		filepath.Join("hub", "business", "v1", "types.rs"),
		filepath.Join("hub", "business", "v1", "client.rs"),
		filepath.Join("hub", "business", "v1", "server.rs"),
	} {
		if _, err := os.Stat(filepath.Join(generated, rel)); err != nil {
			t.Fatalf("expected generated Rust file %s: %v", rel, err)
		}
	}

	businessTypes, err := os.ReadFile(filepath.Join(generated, "hub", "business", "v1", "types.rs"))
	if err != nil {
		t.Fatalf("read Rust business types: %v", err)
	}
	if !strings.Contains(string(businessTypes), "super::super::super::super::common::types::Money") {
		t.Fatalf("expected cross-directory Rust type path, got:\n%s", businessTypes)
	}

	businessClient, err := os.ReadFile(filepath.Join(generated, "hub", "business", "v1", "client.rs"))
	if err != nil {
		t.Fatalf("read Rust business client: %v", err)
	}
	if !strings.Contains(string(businessClient), "/api/hub/business/v1/businesses/{id}") {
		t.Fatalf("expected route prefix in Rust client, got:\n%s", businessClient)
	}

	writeTestFile(t, filepath.Join(dir, "Cargo.toml"), rustCargoToml)
	writeTestFile(t, filepath.Join(dir, "src", "lib.rs"), rustServerHarness)
	cmd := exec.Command("cargo", "test", "--quiet")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Rust crate failed to compile: %v\n%s", err, out)
	}
}
