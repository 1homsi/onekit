package genpy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onktest"
)

const pyCommonSrc = `namespace common;

model Money {
  @encode(string)
  amount_cents: int64;
  currency: string;
}
`

const pyOrdersSrc = `using common;


namespace orders;

model GetOrderRequest {
  id: string;
}

model Order {
  id: string;
  price: Money;
}

@route("/orders/v1")
interface OrderService {
  @get
  @route("/orders/{id}")
  getOrder(...GetOrderRequest): Order;
}
`

// pyDirResolver is a minimal PackageResolver keyed by which directory a
// message or enum was declared in, used to prove cross-package Python
// generation without depending on the onkt CLI's own directory-grouping.
type pyDirResolver struct {
	currentDir   string
	dirByMessage map[*onkir.Message]string
	packages     map[string]PackageRef
}

func (r *pyDirResolver) ResolveMessage(m *onkir.Message) (PackageRef, bool) {
	dir, ok := r.dirByMessage[m]
	if !ok || dir == r.currentDir {
		return PackageRef{}, false
	}
	ref, ok := r.packages[dir]
	return ref, ok
}

func (r *pyDirResolver) ResolveEnum(*onkir.Enum) (PackageRef, bool) {
	return PackageRef{}, false
}

func compilePyCrossPackageFixture(t *testing.T) (*onkir.File, *onkir.File, map[*onkir.Message]string) {
	t.Helper()
	// The files compile together, as one TypeSpec project.
	var sources []onkcompile.Source
	for _, file := range onktest.Files(t, map[string]string{"common/money.tsp": pyCommonSrc, "orders/v1/service.tsp": pyOrdersSrc}) {
		sources = append(sources, onkcompile.Source{Path: file.Path, AST: &file.File})
	}
	pkg, err := onkcompile.Compile(sources)
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

func TestCrossPackagePythonGeneration(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	commonFile, ordersFile, dirByMessage := compilePyCrossPackageFixture(t)

	packages := map[string]PackageRef{
		"common":    {Alias: "common_models", ModulePath: "common.models"},
		"orders/v1": {Alias: "orders_models", ModulePath: "orders.v1.models"},
	}

	commonResolver := &pyDirResolver{currentDir: "common", dirByMessage: dirByMessage, packages: packages}
	ordersResolver := &pyDirResolver{currentDir: "orders/v1", dirByMessage: dirByMessage, packages: packages}

	commonTypes := GenerateTypesWithResolver(commonFile, commonResolver)
	ordersTypes := GenerateTypesWithResolver(ordersFile, ordersResolver)
	ordersClient := GenerateClientWithResolver(ordersFile, "models", ordersResolver)

	if !containsString(string(ordersTypes), "import common.models as common_models") {
		t.Fatalf("expected orders/models.py to import the common package, got:\n%s", ordersTypes)
	}
	if !containsString(string(ordersTypes), "common_models.Money") {
		t.Fatalf("expected orders/models.py to reference common_models.Money, got:\n%s", ordersTypes)
	}

	dir := t.TempDir()
	if mkErr := os.MkdirAll(filepath.Join(dir, "common"), 0o755); mkErr != nil {
		t.Fatalf("mkdir common: %v", mkErr)
	}
	if mkErr := os.MkdirAll(filepath.Join(dir, "orders", "v1"), 0o755); mkErr != nil {
		t.Fatalf("mkdir orders/v1: %v", mkErr)
	}
	writeFile(t, filepath.Join(dir, "common", "__init__.py"), "")
	writeFile(t, filepath.Join(dir, "common", "models.py"), string(commonTypes))
	writeFile(t, filepath.Join(dir, "orders", "__init__.py"), "")
	writeFile(t, filepath.Join(dir, "orders", "v1", "__init__.py"), "")
	writeFile(t, filepath.Join(dir, "orders", "v1", "models.py"), string(ordersTypes))
	writeFile(t, filepath.Join(dir, "orders", "v1", "client.py"), string(ordersClient))
	writeFile(t, filepath.Join(dir, "main.py"), pyCrossPackageHarness)

	cmd := exec.Command("python3", "main.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python run failed: %v\n%s", err, out)
	}
	if got := strings.TrimRight(string(out), "\r\n"); got != "OK" {
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

const pyCrossPackageHarness = `
from common.models import Money
from orders.v1.models import Order


def main():
    order = Order(id="o1", price=Money(amount_cents=1999, currency="USD"))
    d = order.to_dict()
    if d.get("price") != {"amount_cents": "1999", "currency": "USD"}:
        print("unexpected dict:", d)
        raise SystemExit(1)

    order2 = Order.from_dict(d)
    if order2.price is None or order2.price.amount_cents != 1999 or order2.price.currency != "USD":
        print("round trip mismatch:", order2)
        raise SystemExit(1)

    print("OK")


if __name__ == "__main__":
    main()
`
