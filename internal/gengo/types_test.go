package gengo

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkir"
)

func TestGoPackageNameSanitizesReservedSegments(t *testing.T) {
	tests := []struct {
		pkg, want string
	}{
		{"api", "api"},
		{"example.users", "users"},
		{"go", "pkg_go"},
		{"example.gen.go", "pkg_go"},
		{"", "generated"},
		{"3d", "pkg_3d"},
	}
	for _, tt := range tests {
		if got := GoPackageName(&onkir.File{Package: tt.pkg}); got != tt.want {
			t.Errorf("GoPackageName(%q) = %q, want %q", tt.pkg, got, tt.want)
		}
	}
}
