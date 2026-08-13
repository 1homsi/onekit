package onek

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onklang"
)

// Diagnostic is the stable machine-readable shape used by `onek check
// --json`. Paths are kept absolute when the caller supplied an absolute path
// and otherwise remain relative to preserve useful project-local output.
type Diagnostic struct {
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ParseDiagnosticError attaches the source path to a parser/lexer error while
// retaining the original error for errors.As and normal error wrapping.
type ParseDiagnosticError struct {
	Path string
	Err  error
}

func (e *ParseDiagnosticError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("parse %s: %v", e.Path, e.Err)
}

func (e *ParseDiagnosticError) Unwrap() error { return e.Err }

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
	var parseErr *ParseDiagnosticError
	if errors.As(err, &parseErr) {
		diagnostic := Diagnostic{Path: parseErr.Path, Code: "parse_error", Message: parseErr.Err.Error()}
		var syntaxErr *onklang.Error
		if errors.As(parseErr.Err, &syntaxErr) {
			diagnostic.Line = syntaxErr.Line
			diagnostic.Column = syntaxErr.Column
			diagnostic.Message = syntaxErr.Message
		}
		return []Diagnostic{diagnostic}
	}
	var configErr *ConfigError
	if errors.As(err, &configErr) {
		return []Diagnostic{{Path: configErr.Path, Code: "config_error", Message: configErr.Err.Error()}}
	}
	var formatErr *FormatError
	if errors.As(err, &formatErr) {
		diagnostics := make([]Diagnostic, 0, len(formatErr.Files))
		for _, path := range formatErr.Files {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Code: "format_error", Message: "schema file is not formatted"})
		}
		return diagnostics
	}
	return []Diagnostic{{Path: filepath.Clean("."), Code: "tool_error", Message: err.Error()}}
}
