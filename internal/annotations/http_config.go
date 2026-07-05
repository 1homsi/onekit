package annotations

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/corezio/onekit/http"
)

// ErrNoBodyField means the HTTP body binding uses the whole request message.
var ErrNoBodyField = errors.New("onekit: no selected body field")

func IsNoBodyField(err error) bool {
	return errors.Is(err, ErrNoBodyField)
}

// HTTPConfig represents the HTTP configuration for a method.
type HTTPConfig struct {
	Path       string
	Method     string   // "GET", "POST", "PUT", "DELETE", "PATCH"
	PathParams []string // Path variable names extracted from path
	Stream     bool     // When true, this method uses SSE streaming
	Body       string   // "" or "*" = whole message; otherwise the request field that is the body
}

// ServiceConfig represents the HTTP configuration for a service.
type ServiceConfig struct {
	BasePath string
}

// GetMethodHTTPConfig extracts HTTP configuration from method options.
// Returns nil if no HTTP config annotation is present.
func GetMethodHTTPConfig(method *protogen.Method) *HTTPConfig {
	options := method.Desc.Options()
	if options == nil {
		return nil
	}

	methodOptions, ok := options.(*descriptorpb.MethodOptions)
	if !ok {
		return nil
	}

	ext := proto.GetExtension(methodOptions, http.E_Config)
	if ext == nil {
		return nil
	}

	httpConfig, ok := ext.(*http.HttpConfig)
	if !ok || httpConfig == nil {
		return nil
	}

	path := httpConfig.GetPath()

	return &HTTPConfig{
		Path:       path,
		Method:     HTTPMethodToString(httpConfig.GetMethod()),
		PathParams: ExtractPathParams(path),
		Stream:     httpConfig.GetStream(),
		Body:       httpConfig.GetBody(),
	}
}

// GetBodyField resolves the body annotation of a method to the request field
// selected as the HTTP body. Returns nil when the whole message is the body
// ("" or "*"). Returns an error if the named field does not exist or is not a
// singular message field.
func GetBodyField(method *protogen.Method) (*protogen.Field, error) {
	cfg := GetMethodHTTPConfig(method)
	if cfg == nil || cfg.Body == "" || cfg.Body == "*" {
		return nil, ErrNoBodyField
	}
	for _, field := range method.Input.Fields {
		if string(field.Desc.Name()) != cfg.Body {
			continue
		}
		if field.Message == nil || field.Desc.IsList() || field.Desc.IsMap() {
			return nil, fmt.Errorf(
				"method %s: body field %q must be a singular message field",
				method.Desc.FullName(), cfg.Body,
			)
		}
		return field, nil
	}
	return nil, fmt.Errorf(
		"method %s: body field %q not found on request message %s",
		method.Desc.FullName(), cfg.Body, method.Input.Desc.FullName(),
	)
}

// GetServiceBasePath extracts the base path from service options.
// Returns an empty string if no service config annotation is present.
func GetServiceBasePath(service *protogen.Service) string {
	options := service.Desc.Options()
	if options == nil {
		return ""
	}

	serviceOptions, ok := options.(*descriptorpb.ServiceOptions)
	if !ok {
		return ""
	}

	ext := proto.GetExtension(serviceOptions, http.E_ServiceConfig)
	if ext == nil {
		return ""
	}

	serviceConfig, ok := ext.(*http.ServiceConfig)
	if !ok || serviceConfig == nil {
		return ""
	}

	return serviceConfig.GetBasePath()
}
