package gents

import (
	"strings"
	"testing"
)

func TestTSIntegerInValidation(t *testing.T) {
	runTSSchema(t, `
package app
message Page {
  size: int32 @in(10, 25, 50)
  version: int64? @in(1, 2)
}
`, `
import { validatePage } from "./types.ts";

if (validatePage({ size: 25, version: "2" } as any).length !== 0) throw new Error("valid page rejected");
if (validatePage({ size: 30 } as any).length !== 1) throw new Error("size 30 accepted");
if (validatePage({ size: 10, version: "3" } as any).length !== 1) throw new Error("version 3 accepted");
console.log("OK");
`)
}

func TestMSWUsesTheFirstAllowedIntegerValue(t *testing.T) {
	file := compileTSSchema(t, `package app
message Page {
  size: int32 @in(25, 50)
  version: int64 @in(3, 4)
}
service S { get(Page) -> Page @get("/p") }
`)
	out := string(GenerateMSWHandlers(file))
	for _, want := range []string{`"size": 25`, `"version": "3"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in:\n%s", want, out)
		}
	}
}
