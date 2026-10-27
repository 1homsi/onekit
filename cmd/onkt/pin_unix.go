//go:build !windows

package main

import (
	"os"
	"syscall"
)

//nolint:gosec // path is a checksum-verified file in the release cache
func execBinary(path string, args []string) error {
	return syscall.Exec(path, append([]string{path}, args...), os.Environ())
}
