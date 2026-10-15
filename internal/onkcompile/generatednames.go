package onkcompile

import (
	"fmt"
	"path/filepath"
)

var goGeneratedTypeNames = map[string]bool{
	"RequestMetadata": true, "ScopeError": true, "Middleware": true, "RequestIDGenerator": true,
	"Authorizer": true, "RequestResult": true, "RequestObserver": true, "ServerOption": true,
	"ServerOptions": true, "ServerError": true, "ErrorWriter": true, "ResponseControl": true,
	"SSESender": true, "ValidationErrors": true, "UnexpectedStatusError": true, "CallOption": true,
	"EventStream": true,
}

func validateGeneratedNames(sources []Source, options CompileOptions) error {
	if !options.generates("go-server", "go-client") {
		return nil
	}
	serviceTypes := map[string]map[string]string{}
	for _, src := range sources {
		dir := filepath.Dir(src.Path)
		for _, service := range src.AST.Services {
			if serviceTypes[dir] == nil {
				serviceTypes[dir] = map[string]string{}
			}
			serviceTypes[dir][service.Name+"Server"] = service.Name
			serviceTypes[dir][service.Name+"Client"] = service.Name
		}
	}
	for _, src := range sources {
		dir := filepath.Dir(src.Path)
		check := func(name string, line int, kind string) error {
			if goGeneratedTypeNames[name] {
				return &Error{Path: src.Path, Line: line, Msg: fmt.Sprintf("%s name %q collides with a type the Go generators emit; rename it", kind, name)}
			}
			if service, ok := serviceTypes[dir][name]; ok {
				return &Error{Path: src.Path, Line: line, Msg: fmt.Sprintf("%s name %q collides with the type generated for service %s; rename it", kind, name, service)}
			}
			return nil
		}
		for _, message := range src.AST.Messages {
			if err := check(message.Name, message.Line, "message"); err != nil {
				return err
			}
		}
		for _, enum := range src.AST.Enums {
			if err := check(enum.Name, enum.Line, "enum"); err != nil {
				return err
			}
		}
	}
	return nil
}
