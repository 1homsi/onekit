package gents

import (
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onktest"
)

const nullableResponseSchema = `namespace app;

model App {
  id: int64;
}

model Folder {
  id: int64;
  parent_id?: int64 | null;
  app?: App | null;
  note?: string;
}

model FolderList {
  folders: Folder[];
}

model Shared {
  id: int64;
  parent_id?: int64 | null;
}

model Empty {}

@route("/v1")
interface Folders {
  @get
  @route("/folders")
  list(...Empty): FolderList;

  @get
  @route("/shared/{id}")
  get(...Shared): Shared;

  @post
  @route("/shared")
  save(...Shared): Shared;
}
`

func TestTSNullableFieldsOnResponseOnlyMessagesAreRequired(t *testing.T) {
	ast, err := onktest.Parse(nullableResponseSchema)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "api.tsp", AST: ast}})
	if err != nil {
		t.Fatal(err)
	}
	types := string(GenerateTypes(pkg.Files[0]))
	body := func(name string) string {
		start := strings.Index(types, "export interface "+name+" {")
		if start < 0 {
			t.Fatalf("no interface %s", name)
		}
		return types[start : start+strings.Index(types[start:], "\n}")]
	}
	folder := body("Folder")
	for _, want := range []string{"parentId: number | null;", "app: App | null;", "note?: string | undefined;"} {
		if !strings.Contains(folder, want) {
			t.Errorf("response-only Folder lacks %q:\n%s", want, folder)
		}
	}
	if shared := body("Shared"); !strings.Contains(shared, "parentId?: number | null | undefined;") {
		t.Errorf("a message that is also a request stays optional:\n%s", shared)
	}
}

func TestTSNullableDecoderMapsMissingToNull(t *testing.T) {
	runTSSchema(t, nullableResponseSchema, `
import { FoldersClient } from "./client.ts";

const client = new FoldersClient("http://api", {
  fetch: (async () => new Response(JSON.stringify({ folders: [{ id: 1 }, { id: 2, parent_id: 1, app: { id: 9 } }, { id: 3, parent_id: null, app: null }] }), { status: 200 })) as typeof fetch,
});
const list = await client.list({});
const [a, b, c] = list.folders;
const parent: number | null = a.parentId;
if (parent !== null || a.app !== null) throw new Error("a missing key must decode to null");
if (b.parentId !== 1 || b.app?.id !== 9) throw new Error("values must survive");
if (c.parentId !== null || c.app !== null) throw new Error("explicit null must stay null");
if (a.note !== undefined) throw new Error("a plain optional field stays undefined");
console.log("OK");
`)
}
