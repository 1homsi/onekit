package gents

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const zodFixture = `
package zfix

enum State {
  UNKNOWN
  READY @json("ready")
}

message Meta {
  street: string
}

message Request {
  email_addr: string @email
  user_id: string @uuid
  site: string @uri
  code: string @pattern("^[A-Z]{3}$")
  nickname: string @len(2, 8)
  role: string @in("admin", "viewer") @required
  note: string?
  attempts: int32
  big_id: int64
  encoded_big: int64 @encode(number)
  state: State
  state_num: State @encode(number)
  seen_at: timestamp @encode(unix_seconds)
  labels: string[] @min_items(1) @max_items(5)
  attrs: map[string, string]
  payload: json
  meta: Meta @flatten(prefix: "m_")
  choice: oneof(discriminator: "kind") {
    text_case: string
    nested: Meta
  }
}
`

func compileForTest(src string) (*onkir.File, error) {
	ast, err := onklang.Parse(src)
	if err != nil {
		return nil, err
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "fixture.onk", AST: ast}})
	if err != nil {
		return nil, err
	}
	return pkg.Files[0], nil
}

func TestGenerateZodMirrorsValidators(t *testing.T) {
	file, err := compileForTest(zodFixture)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateZod(file))
	for _, want := range []string{
		`import { z } from "zod";`,
		`export const StateSchema = z.enum(["UNKNOWN", "ready"]);`,
		`emailAddr: z.string().email(),`,
		`userId: z.string().uuid(),`,
		`site: z.string().url(),`,
		`code: z.string().regex(new RegExp("^[A-Z]{3}$")),`,
		`nickname: z.string().min(2).max(8),`,
		// @in replaces the base schema entirely; membership implies non-empty
		`role: z.enum(["admin", "viewer"]),`,
		`note: z.string().optional(),`,
		`attempts: z.number().int().gte(-2147483648).lte(2147483647),`,
		`bigId: z.string().regex(/^-?(0|[1-9][0-9]*)$/),`,
		`encodedBig: z.number().int().gte(Number.MIN_SAFE_INTEGER).lte(Number.MAX_SAFE_INTEGER),`,
		`state: StateSchema,`,
		`stateNum: z.number().int().gte(0).lte(1),`,
		`seenAt: z.number(),`,
		`labels: z.array(z.string()).min(1).max(5),`,
		`attrs: z.record(z.string(), z.string()),`,
		`payload: z.unknown(),`,
		// flattened child leaf keeps its combined camelCase key
		`mStreet: z.string(),`,
		// oneof becomes a discriminated union over the variant wire shapes
		`RequestChoiceSchema = z.discriminatedUnion("kind",`,
		`z.object({ kind: z.literal("text_case"), textCase: z.string() })`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated schemas.ts missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateZodUnwrapAndNullable(t *testing.T) {
	src := `
package ufix

message Inner { value: string }

message IdList {
  ids: Inner @unwrap
}

message Holder {
  child: Inner @empty(null)
}
`
	file, err := compileForTest(src)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text := string(GenerateZod(file))
	if !strings.Contains(text, "export const IdListSchema = InnerSchema;") {
		t.Fatalf("@unwrap should alias the inner schema:\n%s", text)
	}
	if !strings.Contains(text, `child: InnerSchema.nullable(),`) {
		t.Fatalf("@empty(null) should emit .nullable():\n%s", text)
	}
}

// TestGenerateZodCrossPackageImportsSchemasModule pins that cross-package
// references import the sibling schemas module rather than types.
func TestGenerateZodCrossPackageImportsSchemasModule(t *testing.T) {
	_, ordersFile, dirByMessage := compileTSCrossPackageFixture(t)
	resolver := &tsDirResolver{
		currentDir:   "orders/v1",
		dirByMessage: dirByMessage,
		packages: map[string]PackageRef{
			"common": {Alias: "common", ImportPath: "../../../common/types"},
		},
	}
	text := string(GenerateZodWithResolver(ordersFile, resolver))
	if !strings.Contains(text, `import * as common from "../../../common/schemas";`) {
		t.Fatalf("expected schemas-module import:\n%s", text)
	}
	if !strings.Contains(text, "price: common.MoneySchema,") {
		t.Fatalf("expected qualified schema reference:\n%s", text)
	}
}
