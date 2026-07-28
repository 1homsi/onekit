package genrust

import "github.com/1homsi/onekit/internal/onkir"

// PackageRef is a module path from the generated Rust source file to another
// generated directory's types module.
type PackageRef struct {
	ModulePath string
}

type PackageResolver interface {
	ResolveMessage(message *onkir.Message) (PackageRef, bool)
	ResolveEnum(enum *onkir.Enum) (PackageRef, bool)
}
