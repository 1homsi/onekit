package onek

import (
	"fmt"
	"path/filepath"

	"github.com/1homsi/onekit/internal/genopenapi"
	"github.com/1homsi/onekit/internal/onkir"
)

// Match the other generators' schema-relative directory layout. When a
// directory declares multiple services, name each document after its service.
func openAPIBasePath(group *sourceGroup, service *onkir.Service) string {
	name := "openapi"
	if len(group.file.Services) > 1 {
		name = service.Name + ".openapi"
	}
	return filepath.Join(filepath.FromSlash(group.relDir), name)
}

// Include the service's transitive type dependencies so every document is
// standalone, even when requests reference shared or recursive messages.
func openAPIServiceFile(service *onkir.Service) *onkir.File {
	file := &onkir.File{Services: []*onkir.Service{service}}
	messages := map[*onkir.Message]bool{}
	enums := map[*onkir.Enum]bool{}
	var visitMessage func(*onkir.Message)
	var visitType func(*onkir.Type)
	visitType = func(typ *onkir.Type) {
		if typ == nil {
			return
		}
		switch typ.Kind {
		case onkir.KindMessage:
			visitMessage(typ.Message)
		case onkir.KindEnum:
			if typ.Enum == nil {
				return
			}
			if typ.Enum.Parent != nil {
				visitMessage(typ.Enum.Parent)
			} else if !enums[typ.Enum] {
				enums[typ.Enum] = true
				file.Enums = append(file.Enums, typ.Enum)
			}
		case onkir.KindMap:
			visitType(typ.MapValue)
		}
	}
	visitMessage = func(message *onkir.Message) {
		if message == nil || messages[message] {
			return
		}
		// collectSchemas emits nested declarations together with their parent.
		if message.Parent != nil && !messages[message.Parent] {
			visitMessage(message.Parent)
			return
		}
		messages[message] = true
		if message.Parent == nil {
			file.Messages = append(file.Messages, message)
		}
		for _, field := range message.Fields {
			visitType(field.Type)
			if field.Oneof != nil {
				for _, variant := range field.Oneof.Variants {
					visitType(variant.Type)
				}
			}
		}
		for _, nested := range message.Nested {
			visitMessage(nested)
		}
	}
	for _, method := range service.Methods {
		visitMessage(method.Request)
		visitMessage(method.Response)
		for _, message := range method.ErrorTypes {
			visitMessage(message)
		}
	}
	return file
}

func buildOpenAPI(cfg *Config, idx *sourceIndex) error {
	opts := genopenapi.Options{Title: cfg.Generate.OpenAPI.Title, Version: cfg.Generate.OpenAPI.Version, Description: cfg.Generate.OpenAPI.Description}
	for _, group := range idx.groups {
		for _, service := range group.file.Services {
			file := openAPIServiceFile(service)
			base := filepath.Join(cfg.resolve(cfg.Generate.OpenAPI.Out), openAPIBasePath(group, service))
			data, err := genopenapi.Generate(file, opts)
			if err != nil {
				return fmt.Errorf("generate openapi for %s: %w", service.Name, err)
			}
			jsonData, err := genopenapi.GenerateJSON(file, opts)
			if err != nil {
				return fmt.Errorf("generate openapi json for %s: %w", service.Name, err)
			}
			if err := writeFile(base+".yaml", data); err != nil {
				return err
			}
			if err := writeFile(base+".json", jsonData); err != nil {
				return err
			}
		}
	}
	return nil
}
