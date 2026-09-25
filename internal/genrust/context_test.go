package genrust

import "testing"

const rustContextFixture = `
package app
message Who { id: string }
service Users { whoami(Who) -> Who @get("/users/{id}") }
`

const rustContextMain = `
mod generated;

use generated::server::*;
use generated::types::*;
use std::sync::Arc;

#[derive(Clone)]
struct CurrentUser(String);

struct Impl;

impl Users for Impl {
    fn whoami(&self, context: RequestContext, req: Who) -> impl std::future::Future<Output = Result<Who, UsersWhoamiServerError>> + Send {
        async move {
            let user = context.extensions.get::<CurrentUser>().map(|u| u.0.clone()).unwrap_or_default();
            Ok(Who { id: format!("{}:{}:{}:{}", req.id, user, context.method, context.uri.path()) })
        }
    }
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let app = users_router(Arc::new(Impl)).layer(axum::Extension(CurrentUser("ada".into())));
    tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });
    let body = reqwest::get(format!("http://{addr}/users/7")).await.unwrap().text().await.unwrap();
    if !body.contains("7:ada:GET:/users/7") {
        eprintln!("context missing request data: {body}");
        std::process::exit(1);
    }
    println!("OK");
}
`

func TestGeneratedRustRequestContextCarriesRequestParts(t *testing.T) {
	runRustWSCrate(t, rustContextFixture, "onekit-rust-context", rustContextMain, true)
}
