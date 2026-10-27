import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace app;

enum Level {
  LOW,
  HIGH,
  MAX,
}

model Leaf {
  name: string;
  n: int32;
}

@Onkt.discriminator("type")
union KitchenChoice {
  @Onkt.tag("text") text: string,
  @Onkt.tag("count") count: int64,
  @Onkt.tag("leaf") item: Leaf,
  @Onkt.tag("level") level: Level,
}

model Kitchen {
  s: string;
  b: boolean;
  i32: int32;
  u32: uint32;
  @encode(string)
  i64: int64;
  @encode(string)
  u64: uint64;
  i64n: int64;
  f32: float32;
  f64: float64;
  lvl: Level;
  @Onkt.encode("number")
  lvln: Level;
  data: bytes;
  @Onkt.encode("hex")
  hexd: bytes;
  @encode("base64url")
  b64u: bytes;
  at: utcDateTime;
  @encode("unixTimestamp", int64)
  atu: utcDateTime;
  @Onkt.encode("unix_millis")
  atm: utcDateTime;
  on: plainDate;
  cfg: unknown;
  @Onkt.object
  obj: unknown;
  leaf: Leaf;
  tags: string[];
  ids: int32[];
  @Onkt.encode("string")
  big: int64[];
  levels: Level[];
  leaves: Leaf[];
  blobs: bytes[];
  m_str: Record<string>;
  m_leaf: Record<Leaf>;
  m_num: Record<int64>;
  @Onkt.mapKey("int64")
  m_int: Record<string>;
  o_s?: string;
  @encode(string)
  o_i?: int64;
  o_in?: int64;
  o_f?: float64;
  o_lvl?: Level;
  o_data?: bytes;
  @Onkt.encode("hex")
  o_hex?: bytes;
  o_at?: utcDateTime;
  @encode("unixTimestamp", int64)
  o_atu?: utcDateTime;
  n_s?: string | null;
  n_in?: int64 | null;
  n_leaf?: Leaf | null;
  n_lvl?: Level | null;
  choice: KitchenChoice;
}

model Holder {
  name: string;
  kitchen: Kitchen;
  items: Kitchen[];
  by_key: Record<Kitchen>;
  leaves: Leaf[];
}

model Tree {
  label: string;
  kids: Tree[];
  extra: Record<string>;
  note?: string | null;
}

model Plain {
  a: string;
  b: int32;
  leaf: Leaf;
}
