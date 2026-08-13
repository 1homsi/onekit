package gengo

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
  optional_amount: int64?
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

message Document {
  data: bytes
  hash: bytes @encode(hex)
  optional_hash: bytes? @encode(hex)
}

message Event {
  created_at: timestamp
  unix_at: timestamp @encode(unix_seconds)
  optional_unix_at: timestamp? @encode(unix_seconds)
  optional_millis_at: timestamp? @encode(unix_millis)
  optional_date: timestamp? @encode(date)
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
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	// int64 default string, @encode(number), repeated string
	optionalAmount := int64(77)
	money := &Money{AmountCents: 12345, AmountNumber: 999, OptionalAmount: &optionalAmount, Amounts: []int64{1, 2, 3}}
	b, err := json.Marshal(money)
	if err != nil {
		fail("marshal Money: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "\"amount_cents\":\"12345\"") {
		fail("expected amount_cents as string, got %s", s)
	}
	if !strings.Contains(s, "\"optional_amount\":\"77\"") {
		fail("expected optional_amount as string, got %s", s)
	}
	if !strings.Contains(s, "\"amount_number\":999") {
		fail("expected amount_number as number, got %s", s)
	}
	if !strings.Contains(s, "\"amounts\":[\"1\",\"2\",\"3\"]") {
		fail("expected amounts as string array, got %s", s)
	}
	var money2 Money
	if err := json.Unmarshal(b, &money2); err != nil {
		fail("unmarshal Money: %v", err)
	}
	if money2.AmountCents != 12345 || money2.AmountNumber != 999 || money2.OptionalAmount == nil || *money2.OptionalAmount != 77 || len(money2.Amounts) != 3 || money2.Amounts[2] != 3 {
		fail("round trip mismatch: %+v", money2)
	}

	// enum default string, @encode(number)
	holder := &StatusHolder{Status: StatusActive, StatusNum: StatusActive}
	b, err = json.Marshal(holder)
	if err != nil {
		fail("marshal StatusHolder: %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"status\":\"active\"") {
		fail("expected status as string, got %s", s)
	}
	if !strings.Contains(s, "\"status_num\":1") {
		fail("expected status_num as number, got %s", s)
	}
	var holder2 StatusHolder
	if err := json.Unmarshal(b, &holder2); err != nil {
		fail("unmarshal StatusHolder: %v", err)
	}
	if holder2.Status != StatusActive || holder2.StatusNum != StatusActive {
		fail("round trip mismatch: %+v", holder2)
	}

	// bytes default base64, @encode(hex)
	optionalHash := []byte("hi")
	doc := &Document{Data: []byte("hi"), Hash: []byte("hi"), OptionalHash: &optionalHash}
	b, err = json.Marshal(doc)
	if err != nil {
		fail("marshal Document: %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"data\":\"aGk=\"") {
		fail("expected base64 data, got %s", s)
	}
	if !strings.Contains(s, "\"hash\":\"6869\"") {
		fail("expected hex hash, got %s", s)
	}
	if !strings.Contains(s, "\"optional_hash\":\"6869\"") {
		fail("expected optional hex hash, got %s", s)
	}
	var doc2 Document
	if err := json.Unmarshal(b, &doc2); err != nil {
		fail("unmarshal Document: %v", err)
	}
	if string(doc2.Data) != "hi" || string(doc2.Hash) != "hi" || doc2.OptionalHash == nil || string(*doc2.OptionalHash) != "hi" {
		fail("round trip mismatch: %+v", doc2)
	}

	// timestamp default rfc3339, @encode(unix_seconds)
	when := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	optionalUnixAt := when
	optionalMillisAt := when
	optionalDate := when
	expectedDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	ev := &Event{CreatedAt: when, UnixAt: when, OptionalUnixAt: &optionalUnixAt, OptionalMillisAt: &optionalMillisAt, OptionalDate: &optionalDate}
	b, err = json.Marshal(ev)
	if err != nil {
		fail("marshal Event: %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"created_at\":\"2024-01-15T09:30:00Z\"") {
		fail("expected rfc3339 created_at, got %s", s)
	}
	if !strings.Contains(s, "\"unix_at\":1705311000") {
		fail("expected unix seconds unix_at, got %s", s)
	}
	if !strings.Contains(s, "\"optional_unix_at\":1705311000") || !strings.Contains(s, "\"optional_millis_at\":1705311000000") || !strings.Contains(s, "\"optional_date\":\"2024-01-15\"") {
		fail("expected optional timestamp encodings, got %s", s)
	}
	var ev2 Event
	if err := json.Unmarshal(b, &ev2); err != nil {
		fail("unmarshal Event: %v", err)
	}
	if !ev2.CreatedAt.Equal(when) || !ev2.UnixAt.Equal(when) || ev2.OptionalUnixAt == nil || !ev2.OptionalUnixAt.Equal(when) || ev2.OptionalMillisAt == nil || !ev2.OptionalMillisAt.Equal(when) || ev2.OptionalDate == nil || !ev2.OptionalDate.Equal(expectedDate) {
		fail("round trip mismatch: created=%v unix=%v", ev2.CreatedAt, ev2.UnixAt)
	}

	// flatten
	order := &Order{Id: "o1", Billing: &Address{Street: "123 Main", City: "Springfield"}}
	b, err = json.Marshal(order)
	if err != nil {
		fail("marshal Order: %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"billing_street\":\"123 Main\"") || !strings.Contains(s, "\"billing_city\":\"Springfield\"") {
		fail("expected flattened billing fields, got %s", s)
	}
	if strings.Contains(s, "\"billing\":{") {
		fail("expected no nested billing object, got %s", s)
	}
	var order2 Order
	if err := json.Unmarshal(b, &order2); err != nil {
		fail("unmarshal Order: %v", err)
	}
	if order2.Billing == nil || order2.Billing.Street != "123 Main" || order2.Billing.City != "Springfield" {
		fail("round trip mismatch: %+v", order2.Billing)
	}

	// empty behavior: null
	respEmpty := &ResponseNull{}
	b, err = json.Marshal(respEmpty)
	if err != nil {
		fail("marshal ResponseNull(empty): %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"meta\":null") {
		fail("expected null meta for empty message, got %s", s)
	}
	respFull := &ResponseNull{Meta: &Meta{Note: "hi"}}
	b, err = json.Marshal(respFull)
	if err != nil {
		fail("marshal ResponseNull(full): %v", err)
	}
	s = string(b)
	if !strings.Contains(s, "\"meta\":{\"note\":\"hi\"}") {
		fail("expected preserved meta for non-empty message, got %s", s)
	}

	// root unwrap
	list := &IdList{Ids: []string{"a", "b", "c"}}
	b, err = json.Marshal(list)
	if err != nil {
		fail("marshal IdList: %v", err)
	}
	s = string(b)
	if s != "[\"a\",\"b\",\"c\"]" {
		fail("expected root-unwrapped array, got %s", s)
	}
	var list2 IdList
	if err := json.Unmarshal(b, &list2); err != nil {
		fail("unmarshal IdList: %v", err)
	}
	if len(list2.Ids) != 3 || list2.Ids[1] != "b" {
		fail("round trip mismatch: %+v", list2)
	}

	fmt.Println("OK")
}
`

func TestJSONMappingAnnotations(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	ast, err := onklang.Parse(jsonMappingFixture)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "app.onk", AST: ast}})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	file := pkg.Files[0]

	typesSrc, err := GenerateTypes(file)
	if err != nil {
		t.Fatalf("GenerateTypes error: %v\n%s", err, typesSrc)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_jsonmapping_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "types.go"), string(typesSrc))
	writeFile(t, filepath.Join(dir, "main.go"), jsonMappingHarness)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "OK\n" {
		t.Fatalf("unexpected program output: %q", got)
	}
}
