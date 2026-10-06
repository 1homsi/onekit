async fn stream_error_event(response: Response, writer: Option<&ErrorWriter>, request_headers: &HeaderMap) -> Event {
    let info = response.extensions().get::<ServerErrorInfo>().cloned();
    let response = match (writer, info) {
        (Some(writer), Some(info)) => writer(&info, request_headers),
        _ => response,
    };
    let bytes = axum::body::to_bytes(response.into_body(), 1 << 20).await.unwrap_or_default();
    match serde_json::from_slice::<serde_json::Value>(&bytes) {
        Ok(value) => Event::default().event("error").json_data(value).unwrap_or_default(),
        Err(_) => Event::default().event("error").data("{\"message\":\"internal server error\"}"),
    }
}

