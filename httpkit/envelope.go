package httpkit

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Failure is what an error writer knows about a failed request. A generated
// ServerError provides the same values through its Parts method.
type Failure struct {
	Status     int
	Code       string
	Message    string
	Field      string
	Violations []string
	Cause      error
}

// Partser is satisfied by the generated ServerError.
type Partser interface {
	Parts() (status int, code, message, field string, violations []string, cause error)
}

// Envelope writes failures as {"error": {"code", "message", "request_id"}}.
// Violations and the offending field are included when present. The cause of
// a 5xx failure is logged and never sent to the client.
type Envelope struct {
	// Logger receives the cause of 5xx failures. Nil uses slog.Default().
	Logger *slog.Logger
	// ContentType defaults to "application/json".
	ContentType string
	// RequestIDHeader names the response header that carries the request id
	// when Middleware is not installed, as generated servers set it with
	// WithRequestID. It defaults to defaultRequestIDHeader.
	RequestIDHeader string
	// Wrap customizes the body. It receives the default body map and may
	// return any value to encode instead.
	Wrap func(f Failure, requestID string, body map[string]any) any
}

// Write sends f as the response.
func (e Envelope) Write(w http.ResponseWriter, r *http.Request, f Failure) {
	requestID := RequestID(r.Context())
	if requestID == "" {
		header := e.RequestIDHeader
		if header == "" {
			header = defaultRequestIDHeader
		}
		requestID = w.Header().Get(header)
	}
	if f.Status >= http.StatusInternalServerError && f.Cause != nil {
		logger := e.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.ErrorContext(r.Context(), "request failed", "status", f.Status, "code", f.Code, "request_id", requestID, "method", r.Method, "path", r.URL.Path, "error", f.Cause)
	}
	inner := map[string]any{"code": f.Code, "message": f.Message}
	if requestID != "" {
		inner["request_id"] = requestID
	}
	if f.Field != "" {
		inner["field"] = f.Field
	}
	if len(f.Violations) > 0 {
		inner["violations"] = f.Violations
	}
	var body any = map[string]any{"error": inner}
	if e.Wrap != nil {
		body = e.Wrap(f, requestID, inner)
	}
	contentType := e.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	data, err := json.Marshal(body)
	if err != nil {
		data = []byte(`{"error":{"code":"internal","message":"internal server error"}}`)
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(f.Status)
	_, _ = w.Write(append(data, '\n'))
}

// ErrorWriter adapts an Envelope to a generated server's error writer:
//
//	api.WithErrorWriter(httpkit.ErrorWriter[*api.ServerError](httpkit.Envelope{}))
func ErrorWriter[E Partser](e Envelope) func(http.ResponseWriter, *http.Request, E) {
	return func(w http.ResponseWriter, r *http.Request, err E) {
		status, code, message, field, violations, cause := err.Parts()
		e.Write(w, r, Failure{Status: status, Code: code, Message: message, Field: field, Violations: violations, Cause: cause})
	}
}
