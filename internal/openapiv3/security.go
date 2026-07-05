package openapiv3

import (
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/corezio/onekit/http"
)

// splitAuthHeaders partitions headers into authentication headers (auth_type set)
// and plain headers. Auth headers become securitySchemes; plain headers become
// regular header parameters.
func splitAuthHeaders(headers []*http.Header) ([]*http.Header, []*http.Header) {
	authHeaders := make([]*http.Header, 0)
	plainHeaders := make([]*http.Header, 0, len(headers))
	for _, header := range headers {
		if header.GetAuthType() != http.AuthType_AUTH_TYPE_UNSPECIFIED {
			authHeaders = append(authHeaders, header)
			continue
		}
		plainHeaders = append(plainHeaders, header)
	}
	return authHeaders, plainHeaders
}

// applySecuritySchemes registers a securityScheme component for every auth header
// and attaches a single security requirement (AND semantics) to the operation.
func (g *Generator) applySecuritySchemes(operation *v3.Operation, authHeaders []*http.Header) {
	if len(authHeaders) == 0 {
		return
	}

	requirements := orderedmap.New[string, []string]()
	for _, header := range authHeaders {
		schemeName := securitySchemeName(header)
		g.registerSecurityScheme(schemeName, header)
		requirements.Set(schemeName, []string{})
	}

	operation.Security = []*base.SecurityRequirement{
		{Requirements: requirements},
	}
}

// registerSecurityScheme adds a securityScheme to components, creating the
// securitySchemes map on first use. Duplicate names are overwritten, which is
// safe because schemes are derived deterministically from the same annotation.
func (g *Generator) registerSecurityScheme(name string, header *http.Header) {
	if g.doc.Components.SecuritySchemes == nil {
		g.doc.Components.SecuritySchemes = orderedmap.New[string, *v3.SecurityScheme]()
	}

	scheme := &v3.SecurityScheme{
		Description: header.GetDescription(),
	}

	switch header.GetAuthType() {
	case http.AuthType_AUTH_TYPE_API_KEY:
		scheme.Type = "apiKey"
		scheme.In = "header"
		scheme.Name = header.GetName()
	case http.AuthType_AUTH_TYPE_BEARER:
		scheme.Type = "http"
		scheme.Scheme = "bearer"
		// The header format annotation (e.g. "jwt") documents the bearer token format.
		scheme.BearerFormat = header.GetFormat()
	case http.AuthType_AUTH_TYPE_BASIC:
		scheme.Type = "http"
		scheme.Scheme = "basic"
	case http.AuthType_AUTH_TYPE_UNSPECIFIED:
		return
	}

	g.doc.Components.SecuritySchemes.Set(name, scheme)
}

// securitySchemeName returns the component name for an auth header's scheme.
// An explicit auth_scheme_name wins; otherwise the name is derived from the
// auth type and header name ("X-API-Key" -> "APIKeyAuth").
func securitySchemeName(header *http.Header) string {
	if header.GetAuthSchemeName() != "" {
		return header.GetAuthSchemeName()
	}
	switch header.GetAuthType() {
	case http.AuthType_AUTH_TYPE_BEARER:
		return "BearerAuth"
	case http.AuthType_AUTH_TYPE_BASIC:
		return "BasicAuth"
	case http.AuthType_AUTH_TYPE_API_KEY, http.AuthType_AUTH_TYPE_UNSPECIFIED:
	}
	name := strings.TrimPrefix(header.GetName(), "X-")
	name = strings.TrimPrefix(name, "x-")
	var b strings.Builder
	for _, part := range strings.Split(name, "-") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	b.WriteString("Auth")
	return b.String()
}
