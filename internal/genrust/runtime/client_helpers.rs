const DEFAULT_MAX_RESPONSE_BODY_BYTES: usize = 8 * 1024 * 1024;
const DEFAULT_MAX_SSE_FRAME_BYTES: usize = 1024 * 1024;

async fn read_response_body(mut response: reqwest::Response, limit: usize) -> Result<Vec<u8>, String> {
    let limit = if limit == 0 { DEFAULT_MAX_RESPONSE_BODY_BYTES } else { limit };
    let mut body = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|error| error.to_string())? {
        if body.len().saturating_add(chunk.len()) > limit { return Err("response body exceeds configured limit".into()); }
        body.extend_from_slice(&chunk);
    }
    Ok(body)
}

#[allow(dead_code)]
fn sse_frame_end(buffer: &[u8]) -> Option<(usize, usize)> {
    let mut i = 0;
    while i < buffer.len() {
        match buffer[i] {
            b'\n' if buffer.get(i + 1) == Some(&b'\n') => return Some((i, 2)),
            b'\n' if buffer.get(i + 1) == Some(&b'\r') && buffer.get(i + 2) == Some(&b'\n') => {
                return Some((i, 3))
            }
            b'\r'
                if buffer.get(i + 1) == Some(&b'\n')
                    && buffer.get(i + 2) == Some(&b'\r')
                    && buffer.get(i + 3) == Some(&b'\n') =>
            {
                return Some((i, 4))
            }
            b'\r' if buffer.get(i + 1) == Some(&b'\r') => return Some((i, 2)),
            _ => {}
        }
        i += 1;
    }
    None
}


#[allow(dead_code)]
fn query_value<T: Serialize>(value: &T) -> String {
    match serde_json::to_value(value).expect("generated request value must serialize") {
        serde_json::Value::String(value) => value,
        serde_json::Value::Null => String::new(),
        value => value.to_string(),
    }
}

