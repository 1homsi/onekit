package gengo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const commonSrc = `
package common

enum Currency {
  UNSPECIFIED
  USD
}

message Money {
  amount_cents: int64
  currency: Currency
}
`

const ordersSrc = `
package orders

message GetOrderRequest {
  id: string
}

message Order {
  id: string
  price: Money
  preferred_currency: Currency
}

service OrderService {
  base_path: "/orders/v1"

  getOrder(GetOrderRequest) -> Order @get("/orders/{id}")
}
`

// dirResolver is a minimal PackageResolver keyed by which directory a message
// or enum was declared in, used to prove cross-package Go generation without
// depending on the onek CLI's own directory-grouping logic.
type dirResolver struct {
	currentDir   string
	dirByMessage map[*onkir.Message]string
	dirByEnum    map[*onkir.Enum]string
	packages     map[string]PackageRef
}

func (r *dirResolver) ResolveMessage(m *onkir.Message) (PackageRef, bool) {
	dir, ok := r.dirByMessage[m]
	if !ok || dir == r.currentDir {
		return PackageRef{}, false
	}
	ref, ok := r.packages[dir]
	return ref, ok
}

func (r *dirResolver) ResolveEnum(e *onkir.Enum) (PackageRef, bool) {
	dir, ok := r.dirByEnum[e]
	if !ok || dir == r.currentDir {
		return PackageRef{}, false
	}
	ref, ok := r.packages[dir]
	return ref, ok
}

func compileCrossPackageFixture(
	t *testing.T,
) (*onkir.File, *onkir.File, map[*onkir.Message]string, map[*onkir.Enum]string) {
	t.Helper()
	commonAST, err := onklang.Parse(commonSrc)
	if err != nil {
		t.Fatalf("parse common: %v", err)
	}
	ordersAST, err := onklang.Parse(ordersSrc)
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
	dirByEnum := map[*onkir.Enum]string{}
	var commonFile, ordersFile *onkir.File
	for _, f := range pkg.Files {
		dir := filepath.ToSlash(filepath.Dir(f.Path))
		for _, m := range f.Messages {
			dirByMessage[m] = dir
		}
		for _, e := range f.Enums {
			dirByEnum[e] = dir
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
	commonFile.Package = "common"
	ordersFile.Package = "v1"

	return commonFile, ordersFile, dirByMessage, dirByEnum
}

func TestCrossPackageGoGeneration(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	commonFile, ordersFile, dirByMessage, dirByEnum := compileCrossPackageFixture(t)

	packages := map[string]PackageRef{
		"common":    {Alias: "common", ImportPath: "onekit_crosspkg_fixture/common"},
		"orders/v1": {Alias: "orders", ImportPath: "onekit_crosspkg_fixture/orders/v1"},
	}

	commonResolver := &dirResolver{
		currentDir: "common", dirByMessage: dirByMessage, dirByEnum: dirByEnum, packages: packages,
	}
	ordersResolver := &dirResolver{
		currentDir: "orders/v1", dirByMessage: dirByMessage, dirByEnum: dirByEnum, packages: packages,
	}

	commonTypes, err := GenerateTypesWithResolver(commonFile, commonResolver)
	if err != nil {
		t.Fatalf("GenerateTypesWithResolver(common) error: %v\n%s", err, commonTypes)
	}

	ordersTypes, err := GenerateTypesWithResolver(ordersFile, ordersResolver)
	if err != nil {
		t.Fatalf("GenerateTypesWithResolver(orders) error: %v\n%s", err, ordersTypes)
	}
	ordersServer, err := GenerateServerWithResolver(ordersFile, ordersResolver)
	if err != nil {
		t.Fatalf("GenerateServerWithResolver(orders) error: %v\n%s", err, ordersServer)
	}
	ordersClient, err := GenerateClientWithResolver(ordersFile, ordersResolver)
	if err != nil {
		t.Fatalf("GenerateClientWithResolver(orders) error: %v\n%s", err, ordersClient)
	}

	if !containsString(string(ordersTypes), `orders_common "onekit_crosspkg_fixture/common"`) &&
		!containsString(string(ordersTypes), `common "onekit_crosspkg_fixture/common"`) {
		t.Fatalf("expected orders/types.go to import the common package, got:\n%s", ordersTypes)
	}
	if !containsString(string(ordersTypes), "common.Money") {
		t.Fatalf("expected orders/types.go to reference common.Money, got:\n%s", ordersTypes)
	}

	if !containsString(string(commonTypes), "common.CurrencyUsd") &&
		!containsString(string(commonTypes), "CurrencyUsd") {
		t.Fatalf("expected common/types.go to declare CurrencyUsd, got:\n%s", commonTypes)
	}
	if !containsString(string(ordersTypes), "common.Currency") {
		t.Fatalf("expected orders/types.go to reference common.Currency (cross-package enum), got:\n%s", ordersTypes)
	}

	dir := t.TempDir()
	if mkErr := os.MkdirAll(filepath.Join(dir, "common"), 0o755); mkErr != nil {
		t.Fatalf("mkdir common: %v", mkErr)
	}
	if mkErr := os.MkdirAll(filepath.Join(dir, "orders", "v1"), 0o755); mkErr != nil {
		t.Fatalf("mkdir orders/v1: %v", mkErr)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module onekit_crosspkg_fixture\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "common", "types.go"), string(commonTypes))
	writeFile(t, filepath.Join(dir, "orders", "v1", "types.go"), string(ordersTypes))
	writeFile(t, filepath.Join(dir, "orders", "v1", "server.go"), string(ordersServer))
	writeFile(t, filepath.Join(dir, "orders", "v1", "client.go"), string(ordersClient))
	writeFile(t, filepath.Join(dir, "main.go"), crossPackageHarness)

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

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

const crossPackageHarness = `
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	common "onekit_crosspkg_fixture/common"
	v1 "onekit_crosspkg_fixture/orders/v1"
)

type impl struct{}

func (impl) GetOrder(ctx context.Context, req *v1.GetOrderRequest) (*v1.Order, error) {
	return &v1.Order{
		Id:    req.Id,
		Price: &common.Money{AmountCents: 1999, Currency: common.CurrencyUsd},
	}, nil
}

func fail(msg string, args ...any) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	mux := http.NewServeMux()
	v1.RegisterOrderServiceServer(mux, impl{})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := v1.NewOrderServiceClient(srv.URL)
	order, err := client.GetOrder(context.Background(), &v1.GetOrderRequest{Id: "o1"})
	if err != nil {
		fail("GetOrder failed: %v", err)
	}
	if order.Id != "o1" || order.Price == nil || order.Price.AmountCents != 1999 || order.Price.Currency != common.CurrencyUsd {
		fail("unexpected order: %+v", order)
	}

	fmt.Println("OK")
}
`
