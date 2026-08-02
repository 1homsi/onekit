package onek

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

func TestInferBasePath(t *testing.T) {
	cases := []struct {
		relDir string
		want   string
	}{
		{".", "/"},
		{"", "/"},
		{"hub/business/v1", "/hub/business/v1"},
		{"hr/time_entry/v1", "/hr/time-entry/v1"},
		{"hr/one_on_one/v1", "/hr/one-on-one/v1"},
		{"pos/menu_item/v1", "/pos/menu-item/v1"},
		{"hr/employees/payroll/runs/v1", "/hr/employees/payroll/runs/v1"},
	}
	for _, c := range cases {
		if got := inferBasePath(c.relDir); got != c.want {
			t.Errorf("inferBasePath(%q) = %q, want %q", c.relDir, got, c.want)
		}
	}
}

func TestApplyRoutePrefix(t *testing.T) {
	pkg := &onkir.Package{Files: []*onkir.File{{Services: []*onkir.Service{
		{BasePath: "/hub/business/v1"},
		{BasePath: "/"},
	}}}}
	applyRoutePrefix(pkg, "/api")

	if got := pkg.Files[0].Services[0].BasePath; got != "/api/hub/business/v1" {
		t.Fatalf("prefixed service base path = %q", got)
	}
	if got := pkg.Files[0].Services[1].BasePath; got != "/api" {
		t.Fatalf("prefixed root service base path = %q", got)
	}
}
