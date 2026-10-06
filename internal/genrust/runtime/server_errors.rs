#[derive(Debug, Clone)]
pub struct ServerErrorInfo {
    pub status: StatusCode,
    pub code: &'static str,
    pub message: String,
    pub violations: Vec<String>,
}

pub type ErrorWriter = Arc<dyn Fn(&ServerErrorInfo, &HeaderMap) -> Response + Send + Sync>;

fn error_response(status: StatusCode, code: &'static str, message: String, violations: Vec<String>) -> Response {
    let body = if violations.is_empty() {
        serde_json::json!({ "message": message })
    } else {
        serde_json::json!({ "message": message, "violations": violations })
    };
    let mut response = (status, Json(body)).into_response();
    response.extensions_mut().insert(ServerErrorInfo { status, code, message, violations });
    response
}

pub fn with_error_writer(router: Router, writer: ErrorWriter) -> Router {
    router.layer(axum::middleware::from_fn(move |mut request: axum::extract::Request, next: axum::middleware::Next| {
        let writer = writer.clone();
        async move {
            let headers = request.headers().clone();
            request.extensions_mut().insert(writer.clone());
            let response = next.run(request).await;
            let info = match response.extensions().get::<ServerErrorInfo>().cloned() {
                Some(info) => info,
                None => {
                    let status = response.status();
                    let (code, message) = match status {
                        StatusCode::NOT_FOUND => ("not_found", "not found"),
                        StatusCode::METHOD_NOT_ALLOWED => ("method_not_allowed", "method not allowed"),
                        _ => return response,
                    };
                    let empty = response.headers().get(axum::http::header::CONTENT_TYPE).is_none();
                    if !empty {
                        return response;
                    }
                    ServerErrorInfo { status, code, message: message.to_string(), violations: Vec::new() }
                }
            };
            let allow = response.headers().get(axum::http::header::ALLOW).cloned();
            let mut replacement = writer(&info, &headers);
            if let Some(allow) = allow {
                replacement.headers_mut().entry(axum::http::header::ALLOW).or_insert(allow);
            }
            replacement
        }
    }))
}
