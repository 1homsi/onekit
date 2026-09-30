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
