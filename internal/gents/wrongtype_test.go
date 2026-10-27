package gents

import "testing"

func TestTSServerAnswers400ForAFieldOfTheWrongType(t *testing.T) {
	runTSSchema(t, `namespace app;

model Guest {
  @minLength(1)
  @maxLength(50)
  name: string;
}

model SignIn {
  @format("email")
  @minLength(3)
  @maxLength(254)
  email: string;
  guests?: Guest[];
  @minValue(1)
  party?: int32;
}

model Done {
  ok: boolean;
}

interface Auth {
  @post
  @route("/sign-in")
  signIn(...SignIn): Done;
}
`, `
import { createAuthFetchHandler } from "./server.ts";

const handler = createAuthFetchHandler({ async signIn() { return { ok: true }; } });
const cases: [string, number, string][] = [
  ['{"email":"guest@example.com"}', 200, ""],
  ['{"email":"x"}', 400, "email must be a valid email; email has invalid length"],
  ['{"email":12345}', 400, "email has invalid type"],
  ['{"email":true}', 400, "email has invalid type"],
  ['{"email":{"a":1}}', 400, "email has invalid type"],
  ['{"email":"guest@example.com","guests":5}', 400, "guests has invalid type"],
  ['{"email":"guest@example.com","guests":{"name":"a"}}', 400, "guests has invalid type"],
  ['{"email":"guest@example.com","guests":[{"name":5}]}', 400, "guests: name has invalid type"],
  ['{"email":"guest@example.com","party":0}', 400, "party violates @gte"],
  ['{"email":"guest@example.com","party":"abc"}', 400, "party has invalid type"],
  ['{"email":"guest@example.com","party":{"valueOf":1,"toString":1}}', 400, "party has invalid type"],
];
for (const [body, status, message] of cases) {
  const res = await handler(new Request("http://x/sign-in", { method: "POST", body, headers: { "content-type": "application/json" } }));
  const got = await res.json();
  if (res.status !== status || (status === 400 && got.message !== message)) throw new Error(body + ": " + res.status + " " + JSON.stringify(got));
}
console.log("OK");
`)
}
