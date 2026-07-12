package gents

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const tsCommonSrc = `
package common

message Money {
  amount_cents: int64
  currency: string
}
`

const tsOrdersSrc = `
package orders

message GetOrderRequest {
  id: string
}

message Order {
  id: string
  price: Money
}

service OrderService {
  base_path: "/orders/v1"

  getOrder(GetOrderRequest) -> Order @get("/orders/{id}")
}
`

// tsDirResolver is a minimal PackageResolver keyed by which directory a
// message or enum was declared in, used to prove cross-package TypeScript
// generation without depending on the onek CLI's own directory-grouping.
type tsDirResolver struct {
	currentDir   string
	dirByMessage map[*onkir.Message]string
	packages     map[string]PackageRef
}

func (r *tsDirResolver) ResolveMessage(m *onkir.Message) (PackageRef, bool) {
	dir, ok := r.dirByMessage[m]
	if !ok || dir == r.currentDir {
		return PackageRef{}, false
	}
	ref, ok := r.packages[dir]
	return ref, ok
}

func (r *tsDirResolver) ResolveEnum(*onkir.Enum) (PackageRef, bool) {
	return PackageRef{}, false
}

func compileTSCrossPackageFixture(t *testing.T) (*onkir.File, *onkir.File, map[*onkir.Message]string) {
	t.Helper()
	commonAST, err := onklang.Parse(tsCommonSrc)
	if err != nil {
		t.Fatalf("parse common: %v", err)
	}
	ordersAST, err := onklang.Parse(tsOrdersSrc)
	if err != nil {
		t.Fatalf("parse orders: %v", err)
	}

	pkg, err := onkcompile.Compile([]onkcompile.Source{
		{Path: "common/money.onk", AST: commonAST},
		{Path: "orders/v1/service.onk", AST: ordersAST},
	})
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	dirByMessage := map[*onkir.Message]string{}
	var commonFile, ordersFile *onkir.File
	for _, f := range pkg.Files {
		dir := filepath.ToSlash(filepath.Dir(f.Path))
		for _, m := range f.Messages {
			dirByMessage[m] = dir
		}
		switch dir {
		case "common":
			commonFile = f
		case "orders/v1":
			ordersFile = f
		}
	}
	if commonFile == nil || ordersFile == nil {
		t.Fatalf("expected both common and orders/v1 files, got %d files", len(pkg.Files))
	}

	return commonFile, ordersFile, dirByMessage
}

func TestCrossPackageTypeScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	commonFile, ordersFile, dirByMessage := compileTSCrossPackageFixture(t)

	// orders/v1 is two levels below the schema root, common is one level
	// below it - so the import path from orders/v1 back to common is "../../common/types".
	packages := map[string]PackageRef{
		"common": {Alias: "common", ImportPath: "../../common/types"},
	}

	commonResolver := &tsDirResolver{currentDir: "common", dirByMessage: dirByMessage, packages: packages}
	ordersResolver := &tsDirResolver{currentDir: "orders/v1", dirByMessage: dirByMessage, packages: packages}

	commonTypes := GenerateTypesWithResolver(commonFile, commonResolver)
	ordersTypes := GenerateTypesWithResolver(ordersFile, ordersResolver)
	ordersClient := GenerateClientWithResolver(ordersFile, ordersResolver)
	ordersServer := GenerateServerWithResolver(ordersFile, ordersResolver)

	if !containsString(string(ordersTypes), `import type * as common from "../../common/types";`) {
		t.Fatalf("expected orders/v1/types.ts to import the common package, got:\n%s", ordersTypes)
	}
	if !containsString(string(ordersTypes), "common.Money") {
		t.Fatalf("expected orders/v1/types.ts to reference common.Money, got:\n%s", ordersTypes)
	}

	dir := t.TempDir()
	if mkErr := os.MkdirAll(filepath.Join(dir, "common"), 0o755); mkErr != nil {
		t.Fatalf("mkdir common: %v", mkErr)
	}
	if mkErr := os.MkdirAll(filepath.Join(dir, "orders", "v1"), 0o755); mkErr != nil {
		t.Fatalf("mkdir orders/v1: %v", mkErr)
	}
	writeFile(t, filepath.Join(dir, "common", "types.ts"), string(commonTypes))
	writeFile(t, filepath.Join(dir, "orders", "v1", "types.ts"), string(ordersTypes))
	writeFile(t, filepath.Join(dir, "orders", "v1", "client.ts"), string(ordersClient))
	writeFile(t, filepath.Join(dir, "orders", "v1", "server.ts"), string(ordersServer))
	writeFile(t, filepath.Join(dir, "main.ts"), tsCrossPackageHarness)

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

func containsString(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}())
}

const tsCrossPackageHarness = `
import { createOrderServiceRoutes } from "./orders/v1/server.ts";
import type { RouteDescriptor } from "./orders/v1/server.ts";
import type { GetOrderRequest, Order } from "./orders/v1/types.ts";

const handler = {
  async getOrder(req: GetOrderRequest): Promise<Order> {
    return { id: req.id, price: { amount_cents: "1999", currency: "USD" } };
  },
};

function findRoute(routes: RouteDescriptor[], method: string, path: string): RouteDescriptor {
  const r = routes.find((r) => r.method === method && r.path === path);
  if (!r) throw new Error("route not found: " + method + " " + path);
  return r;
}

async function main() {
  const routes = createOrderServiceRoutes(handler);
  const route = findRoute(routes, "GET", "/orders/v1/orders/{id}");

  const req = new Request("http://x/orders/v1/orders/o1");
  const res = await route.handler(req);
  if (res.status !== 200) throw new Error("expected 200, got " + res.status);
  const order = (await res.json()) as Order;
  if (order.id !== "o1" || !order.price || order.price.amount_cents !== "1999" || order.price.currency !== "USD") {
    throw new Error("unexpected order: " + JSON.stringify(order));
  }

  console.log("OK");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
