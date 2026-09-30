pub trait WsRawFrame: Sized {
    const WS_RAW: bool;
    fn ws_split_raw(self, raw: &mut Vec<Vec<u8>>) -> Self;
    fn ws_join_raw(&mut self, raw: &mut std::vec::IntoIter<Vec<u8>>) -> bool;
    const WS_TIMEOUT: bool = false;
    fn ws_with_timeout(self, _ms: u64) -> Self {
        self
    }
}

pub const WS_CHUNK_BYTES: usize = 8 << 20;
pub const WS_CHUNK_THRESHOLD: usize = 16 << 20;
pub const DEFAULT_MAX_WS_MESSAGE_BYTES: usize = 256 << 20;

pub enum WsPiece<'a> {
    Whole(&'a [u8]),
    Assembled(bool, Vec<u8>),
    Pending,
}

#[derive(Default)]
pub struct WsAssembler {
    buf: Option<Vec<u8>>,
    total: usize,
    kind: u8,
}

impl WsAssembler {
    pub fn active(&self) -> bool {
        self.buf.is_some()
    }

    pub fn feed<'a>(&mut self, data: &'a [u8], limit: usize) -> Result<WsPiece<'a>, u16> {
        if data.len() < 13 || data[0..4] != [0xff; 4] {
            if self.buf.is_some() {
                return Err(1007);
            }
            return Ok(WsPiece::Whole(data));
        }
        let kind = data[4];
        let mut total_bytes = [0u8; 8];
        total_bytes.copy_from_slice(&data[5..13]);
        let total = u64::from_be_bytes(total_bytes) as usize;
        let chunk = &data[13..];
        if kind > 1 {
            return Err(1007);
        }
        match self.buf.as_mut() {
            None => {
                if total > limit {
                    return Err(1009);
                }
                let mut buf = Vec::with_capacity(total);
                buf.extend_from_slice(chunk);
                self.buf = Some(buf);
                self.total = total;
                self.kind = kind;
            }
            Some(buf) => {
                if total != self.total || kind != self.kind {
                    return Err(1007);
                }
                buf.extend_from_slice(chunk);
            }
        }
        let filled = self.buf.as_ref().map_or(0, Vec::len);
        if filled > self.total {
            return Err(1007);
        }
        if filled < self.total {
            return Ok(WsPiece::Pending);
        }
        Ok(WsPiece::Assembled(
            self.kind == 1,
            self.buf.take().unwrap_or_default(),
        ))
    }
}

pub fn ws_chunks(binary: bool, data: &[u8]) -> Option<Vec<Vec<u8>>> {
    if data.len() <= WS_CHUNK_THRESHOLD {
        return None;
    }
    let mut header = [0u8; 13];
    header[0..4].copy_from_slice(&[0xff; 4]);
    header[4] = u8::from(binary);
    header[5..13].copy_from_slice(&(data.len() as u64).to_be_bytes());
    Some(
        data.chunks(WS_CHUNK_BYTES)
            .map(|part| {
                let mut out = Vec::with_capacity(13 + part.len());
                out.extend_from_slice(&header);
                out.extend_from_slice(part);
                out
            })
            .collect(),
    )
}

pub fn ws_encode_frame<T: WsRawFrame + Serialize>(value: T) -> Result<(bool, Vec<u8>), String> {
    if !T::WS_RAW {
        return serde_json::to_vec(&value)
            .map(|data| (false, data))
            .map_err(|error| error.to_string());
    }
    let mut raw = Vec::new();
    let header = value.ws_split_raw(&mut raw);
    let header_json = serde_json::to_vec(&header).map_err(|error| error.to_string())?;
    let size: usize = raw.iter().map(Vec::len).sum();
    if size == 0 {
        return Ok((false, header_json));
    }
    let mut out = Vec::with_capacity(8 + header_json.len() + 4 * raw.len() + size);
    out.extend_from_slice(&(header_json.len() as u32).to_be_bytes());
    out.extend_from_slice(&header_json);
    out.extend_from_slice(&(raw.len() as u32).to_be_bytes());
    for segment in &raw {
        out.extend_from_slice(&(segment.len() as u32).to_be_bytes());
    }
    for segment in &raw {
        out.extend_from_slice(segment);
    }
    Ok((true, out))
}

pub fn ws_decode_frame<T: WsRawFrame + serde::de::DeserializeOwned>(
    binary: bool,
    data: &[u8],
) -> Result<T, String> {
    if !binary || !T::WS_RAW {
        return serde_json::from_slice(data).map_err(|error| error.to_string());
    }
    let malformed = || "malformed binary frame".to_string();
    let read_u32 = |data: &[u8], at: usize| -> Option<usize> {
        data.get(at..at + 4)
            .map(|b| u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as usize)
    };
    let header_len = read_u32(data, 0).ok_or_else(malformed)?;
    let mut offset = 4usize;
    let header = data
        .get(offset..offset + header_len)
        .ok_or_else(malformed)?;
    offset += header_len;
    let count = read_u32(data, offset).ok_or_else(malformed)?;
    offset += 4;
    let mut lengths = Vec::with_capacity(count.min(data.len() / 4));
    for _ in 0..count {
        lengths.push(read_u32(data, offset).ok_or_else(malformed)?);
        offset += 4;
    }
    let mut raw = Vec::with_capacity(lengths.len());
    for length in lengths {
        raw.push(
            data.get(offset..offset + length)
                .ok_or_else(malformed)?
                .to_vec(),
        );
        offset += length;
    }
    if offset != data.len() {
        return Err(malformed());
    }
    let mut value: T = serde_json::from_slice(header).map_err(|error| error.to_string())?;
    let mut segments = raw.into_iter();
    if !value.ws_join_raw(&mut segments) || segments.next().is_some() {
        return Err(malformed());
    }
    Ok(value)
}
