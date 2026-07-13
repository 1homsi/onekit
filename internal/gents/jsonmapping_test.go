package gents

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

const jsonMappingFixture = `
package main

message Money {
  amount_cents: int64
  amount_number: int64 @encode(number)
  amounts: int64[]
}

enum Status {
  UNSPECIFIED
  ACTIVE @json("active")
}

message StatusHolder {
  status: Status
  status_num: Status @encode(number)
}

message Event {
  created_at: timestamp
  unix_at: timestamp @encode(unix_seconds)
}

message Address {
  street: string
  city: string
}

message Order {
  id: string
  billing: Address @flatten(prefix: "billing_")
}

message Meta {
  note: string
}

message ResponseNull {
  meta: Meta @empty(null)
}

message IdList {
  ids: string[] @unwrap
}
`

const jsonMappingHarness = `
import type { Money, StatusHolder, Event, Order, ResponseNull, IdList } from "./types.ts";
import { encodeMoney, decodeMoney, encodeStatusHolder, decodeStatusHolder, encodeEvent, decodeEvent, encodeOrder, decodeOrder, encodeResponseNull, decodeResponseNull } from "./types.ts";

function fail(msg: string): void {
  console.log(msg);
  process.exit(1);
}

function main() {
  // int64 default string, @encode(number), repeated string - TS side is
  // camelCase, wire side (what encode/JSON.stringify produces) stays
  // snake_case so it still matches what a Go backend expects.
  const money: Money = { amountCents: "12345", amountNumber: 999, amounts: ["1", "2", "3"] };
  const moneyJSON = JSON.stringify(encodeMoney(money));
  if (!moneyJSON.includes('"amount_cents":"12345"')) fail("expected amount_cents as string: " + moneyJSON);
  if (!moneyJSON.includes('"amount_number":999')) fail("expected amount_number as number: " + moneyJSON);
  if (!moneyJSON.includes('"amounts":["1","2","3"]')) fail("expected amounts as string array: " + moneyJSON);
  const decodedMoney = decodeMoney(JSON.parse(moneyJSON));
  if (decodedMoney.amountCents !== "12345" || decodedMoney.amountNumber !== 999) {
    fail("expected decodeMoney round trip: " + JSON.stringify(decodedMoney));
  }

  // enum default string, @encode(number)
  const holder: StatusHolder = { status: "active", statusNum: 1 };
  const holderJSON = JSON.stringify(encodeStatusHolder(holder));
  if (!holderJSON.includes('"status":"active"')) fail("expected status as string: " + holderJSON);
  if (!holderJSON.includes('"status_num":1')) fail("expected status_num as number: " + holderJSON);
  const decodedHolder = decodeStatusHolder(JSON.parse(holderJSON));
  if (decodedHolder.statusNum !== 1) fail("expected decodeStatusHolder round trip: " + JSON.stringify(decodedHolder));

  // timestamp default rfc3339, @encode(unix_seconds)
  const ev: Event = { createdAt: "2024-01-15T09:30:00Z", unixAt: 1705311000 };
  const evJSON = JSON.stringify(encodeEvent(ev));
  if (!evJSON.includes('"created_at":"2024-01-15T09:30:00Z"')) fail("expected rfc3339 created_at: " + evJSON);
  if (!evJSON.includes('"unix_at":1705311000')) fail("expected unix seconds unix_at: " + evJSON);
  const decodedEvent = decodeEvent(JSON.parse(evJSON));
  if (decodedEvent.unixAt !== 1705311000) fail("expected decodeEvent round trip: " + JSON.stringify(decodedEvent));

  // flatten - a flattened field's TS name is prefix+field camelCased as one
  // identifier (billingStreet), while its wire name stays prefix+field
  // snake_case (billing_street) - encode/decode bridge the two, there's no
  // nested "billing" object at either layer.
  const order: Order = { id: "o1", billingStreet: "123 Main", billingCity: "Springfield" };
  const orderJSON = JSON.stringify(encodeOrder(order));
  if (!orderJSON.includes('"billing_street":"123 Main"') || !orderJSON.includes('"billing_city":"Springfield"')) {
    fail("expected flattened billing fields: " + orderJSON);
  }
  if (orderJSON.includes('"billing":{')) fail("expected no nested billing object: " + orderJSON);
  const decodedOrder = decodeOrder(JSON.parse(orderJSON));
  if (decodedOrder.billingStreet !== "123 Main" || decodedOrder.billingCity !== "Springfield") {
    fail("expected decodeOrder round trip: " + JSON.stringify(decodedOrder));
  }

  // empty behavior: null
  const respEmpty: ResponseNull = { meta: {} };
  const encodedEmpty = encodeResponseNull(respEmpty);
  const emptyJSON = JSON.stringify(encodedEmpty);
  if (!emptyJSON.includes('"meta":null')) fail("expected null meta for empty message: " + emptyJSON);
  const decodedEmpty = decodeResponseNull(JSON.parse(emptyJSON));
  if (decodedEmpty.meta !== null) fail("expected decodeResponseNull to preserve null: " + JSON.stringify(decodedEmpty));

  const respFull: ResponseNull = { meta: { note: "hi" } };
  const encodedFull = encodeResponseNull(respFull);
  const fullJSON = JSON.stringify(encodedFull);
  if (!fullJSON.includes('"meta":{"note":"hi"}')) fail("expected preserved meta for non-empty message: " + fullJSON);
  const decodedFull = decodeResponseNull(JSON.parse(fullJSON));
  if (!decodedFull.meta || decodedFull.meta.note !== "hi") {
    fail("expected decodeResponseNull round trip: " + JSON.stringify(decodedFull));
  }

  // root unwrap - the type is a bare array alias
  const list: IdList = ["a", "b", "c"];
  const listJSON = JSON.stringify(list);
  if (listJSON !== '["a","b","c"]') fail("expected root-unwrapped array: " + listJSON);

  console.log("OK");
}

main();
`

func compileJSONMappingFixture(t *testing.T) *onkcompile.Source {
	t.Helper()
	ast, err := onklang.Parse(jsonMappingFixture)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return &onkcompile.Source{Path: "app.onk", AST: ast}
}

func TestJSONMappingTypeScriptTypeChecks(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}

	src := compileJSONMappingFixture(t)
	pkg, err := onkcompile.Compile([]onkcompile.Source{*src})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]
	typesSrc := GenerateTypes(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "lib": ["ES2022", "DOM"]
  }
}
`)

	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc type check failed: %v\n%s", err, out)
	}
}

func TestJSONMappingAnnotationsRuntime(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	src := compileJSONMappingFixture(t)
	pkg, err := onkcompile.Compile([]onkcompile.Source{*src})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]
	typesSrc := GenerateTypes(file)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.ts"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "main.ts"), jsonMappingHarness)

	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
