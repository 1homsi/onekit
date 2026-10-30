package onek

import (
	"errors"
	"path/filepath"

	"github.com/1homsi/onekit/internal/onkcompile"
)

// Diagnostic is the stable machine-readable shape used by `onkt check
// --json`. Paths are kept absolute when the caller supplied an absolute path
// and otherwise remain relative to preserve useful project-local output.
type Diagnostic struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Diagnostics converts one error chain into the JSON diagnostics consumed by
// editors and CI. It intentionally returns one fallback diagnostic for
// configuration and filesystem errors that do not have source locations.
func Diagnostics(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var compileErr *onkcompile.Error
	if errors.As(err, &compileErr) {
		code := compileErr.Code
		if code == "" {
			code = "compile_error"
		}
		return []Diagnostic{{
			Path:    compileErr.Path,
			Line:    compileErr.Line,
			Column:  compileErr.Column,
			Code:    code,
			Message: compileErr.Msg,
		}}
	}
	var configErr *ConfigError
	if errors.As(err, &configErr) {
		return []Diagnostic{{Path: configErr.Path, Line: configErr.Line, Column: configErr.Column, Code: "config_error", Message: configErr.Err.Error()}}
	}
	var typespecErr *TypeSpecError
	if errors.As(err, &typespecErr) {
		if diagnostics := typespecErr.Diagnostics(); len(diagnostics) > 0 {
			return diagnostics
		}
	}
	return []Diagnostic{{Path: filepath.Clean("."), Code: "tool_error", Message: err.Error()}}
}
