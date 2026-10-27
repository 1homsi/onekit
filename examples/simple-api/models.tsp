import "@onkt/typespec";

namespace examples.onksimpleapi.models;

/** A system user. */
model User {
  @format("uuid") id: string;
  @minLength(1) @maxLength(100) name: string;
  @format("email") email: string;
  @encode(string) created_at: int64;
}

model CreateUserRequest {
  @minLength(2) @maxLength(100) name: string;
  @format("email") email: string;
}

model GetUserRequest {
  @format("uuid") id: string;
}

model EmailAuth {
  @format("email") email: string;
  @minLength(8) @maxLength(72) password: string;
  use_two_factor: boolean;
  two_factor_code: string;
}

model TokenAuth {
  @minLength(10) @maxLength(256) token: string;
  token_type: string;
  @encode(string) issued_at: int64;
}

model SocialAuth {
  provider: "google" | "github" | "facebook" | "apple";
  @minLength(20) @maxLength(512) access_token: string;
  redirect_uri: string;
  state: string;
  permissions: string[];
}

@Onkt.discriminator("auth_type")
union AuthMethod {
  email: EmailAuth,
  token: TokenAuth,
  social: SocialAuth,
}

model LoginRequest {
  device_id: string;
  ip_address: string;
  retry_count: int32;
  user_agent: string;
  auth_method: AuthMethod;
  remember_me: boolean;
  session_id: string;
  @encode(string) timestamp: int64;
  scopes: string[];
}

model LoginResponse {
  access_token: string;
  refresh_token: string;
  @encode(string) expires_in: int64;
  user: User;
}
