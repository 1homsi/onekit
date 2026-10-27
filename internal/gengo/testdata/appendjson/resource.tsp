import "@typespec/http";
import "@onkt/typespec";

using TypeSpec.Http;

namespace app;

model Env {
  key: string;
  value: string;
}

model Resource {
  id: string;
  name: string;
  kind: string;
  size: int64;
  ratio: float64;
  active: boolean;
  folder_id?: int64 | null;
  note?: string | null;
  tags: string[];
  headers: Record<string>;
  secret_keys: string[];
  @Onkt.object
  config: unknown;
  created_at: utcDateTime;
  `env`: Env[];
  owner: Env;
}

model ResourceList {
  resources: Resource[];
}
