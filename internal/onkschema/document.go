package onkschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Version is the schema document format this build reads. A frontend writes
// it as the document's "version"; any other value is rejected so an old
// onek never misreads a newer frontend's output.
const Version = 1

// Document is the JSON form of a whole schema project: every source file a
// frontend compiled, in a stable order.
type Document struct {
	Version int          `json:"version"`
	Files   []SourceFile `json:"files"`
}

// SourceFile is one schema file. Path names the frontend source the
// declarations came from; its directory decides the generated package, and
// diagnostics are reported against it. A frontend whose source file holds
// several packages may emit one SourceFile per package with distinct paths
// in the same directory.
type SourceFile struct {
	Path string `json:"path"`
	File
}

// Decode reads a schema document, rejecting unknown keys, trailing data, an
// unsupported version and declarations the compiler cannot represent.
func Decode(r io.Reader) (*Document, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode schema document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode schema document: unexpected data after the document")
	}
	if doc.Version != Version {
		return nil, fmt.Errorf("schema document version %d is not supported (this onek reads version %d)", doc.Version, Version)
	}
	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DecodeBytes is Decode over an in-memory document.
func DecodeBytes(data []byte) (*Document, error) {
	return Decode(bytes.NewReader(data))
}

func (d *Document) validate() error {
	seen := map[string]bool{}
	for i := range d.Files {
		f := &d.Files[i]
		if f.Path == "" {
			return fmt.Errorf("schema document file %d has no path", i)
		}
		if seen[f.Path] {
			return fmt.Errorf("schema document lists %s more than once", f.Path)
		}
		seen[f.Path] = true
		if err := validateFile(f); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
	}
	return nil
}

func validateFile(f *SourceFile) error {
	for _, m := range f.Messages {
		if err := validateMessage(m); err != nil {
			return err
		}
	}
	for _, e := range f.Enums {
		if e == nil || e.Name == "" {
			return errors.New("enum without a name")
		}
	}
	for _, s := range f.Services {
		if s == nil || s.Name == "" {
			return errors.New("service without a name")
		}
		for _, rpc := range s.RPCs {
			if rpc == nil || rpc.Name == "" {
				return fmt.Errorf("service %s has an rpc without a name", s.Name)
			}
		}
	}
	return nil
}

func validateMessage(m *MessageDecl) error {
	if m == nil || m.Name == "" {
		return errors.New("message without a name")
	}
	for _, field := range m.Fields {
		if field == nil || field.Name == "" {
			return fmt.Errorf("message %s has a field without a name", m.Name)
		}
		switch {
		case field.Oneof != nil && field.Type != nil:
			return fmt.Errorf("field %s.%s has both a type and a oneof", m.Name, field.Name)
		case field.Oneof != nil:
			for _, variant := range field.Oneof.Variants {
				if err := validateTypeRef(variant.Type); err != nil {
					return fmt.Errorf("oneof %s.%s variant %s: %w", m.Name, field.Name, variant.Name, err)
				}
			}
		default:
			if err := validateTypeRef(field.Type); err != nil {
				return fmt.Errorf("field %s.%s: %w", m.Name, field.Name, err)
			}
		}
	}
	for _, nested := range m.Nested {
		if err := validateMessage(nested); err != nil {
			return err
		}
	}
	for _, e := range m.NestedEn {
		if e == nil || e.Name == "" {
			return fmt.Errorf("message %s has an enum without a name", m.Name)
		}
	}
	return nil
}

func validateTypeRef(t *TypeRef) error {
	switch {
	case t == nil:
		return errors.New("missing type")
	case t.IsMap:
		if t.MapKey == "" {
			return errors.New("map type without a key type")
		}
		return validateTypeRef(t.MapVal)
	case t.Name == "":
		return errors.New("type without a name")
	}
	return nil
}
