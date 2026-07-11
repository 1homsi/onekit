package annotations

import (
	"strings"

	"github.com/1homsi/onekit/http"
)

// HTTP method string constants (uppercase).
const (
	HTTPMethodGet    = "GET"
	HTTPMethodPost   = "POST"
	HTTPMethodPut    = "PUT"
	HTTPMethodDelete = "DELETE"
	HTTPMethodPatch  = "PATCH"
	HTTPMethodQuery  = "QUERY"
)

// HTTPMethodToString converts HttpMethod enum to an uppercase string.
// Returns "POST" for unspecified or unknown values (backward compatibility).
func HTTPMethodToString(m http.HttpMethod) string {
	switch m {
	case http.HttpMethod_HTTP_METHOD_GET:
		return HTTPMethodGet
	case http.HttpMethod_HTTP_METHOD_POST:
		return HTTPMethodPost
	case http.HttpMethod_HTTP_METHOD_PUT:
		return HTTPMethodPut
	case http.HttpMethod_HTTP_METHOD_DELETE:
		return HTTPMethodDelete
	case http.HttpMethod_HTTP_METHOD_PATCH:
		return HTTPMethodPatch
	case http.HttpMethod_HTTP_METHOD_QUERY:
		return HTTPMethodQuery
	case http.HttpMethod_HTTP_METHOD_UNSPECIFIED:
		// HTTP_METHOD_UNSPECIFIED defaults to POST for backward compatibility
		return HTTPMethodPost
	}
	// Any unknown value defaults to POST for backward compatibility
	return HTTPMethodPost
}

// HTTPMethodToLower converts HttpMethod enum to a lowercase string.
// Returns "post" for unspecified or unknown values (backward compatibility).
// Used by OpenAPI generator which requires lowercase method names.
func HTTPMethodToLower(m http.HttpMethod) string {
	return strings.ToLower(HTTPMethodToString(m))
}
