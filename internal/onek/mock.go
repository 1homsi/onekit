package onek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkir"
)

// MockOptions configures the generated dev server.
type MockOptions struct {
	// Addr is the listen address; defaults to 127.0.0.1:8080 when empty.
	Addr string
	// Seed makes error injection and latency jitter deterministic.
	Seed int64
	// ErrorRate in [0,1] serves declared typed errors instead of success
	// responses with the configured probability.
	ErrorRate float64
	// Latency injects up to this much random delay before responding.
	Latency time.Duration
}

// MockServer serves schema-derived fixtures for every compiled route.
// Fixtures are pure functions of the schema - identical inputs always
// produce byte-identical bodies - while rng drives only latency jitter and
// error-injection draws.
type MockServer struct {
	mux       *http.ServeMux
	rng       *rand.Rand
	errorRate float64
	latency   time.Duration
	routes    int
}

// NewMockServer parses and compiles the project at dir (honoring
// schema_root/route_prefix) and returns a ready-to-serve mock.
func NewMockServer(dir string, opts MockOptions) (*MockServer, error) {
	cfg, err := loadOptionalConfig(dir)
	if err != nil && !errors.Is(err, errNoConfigFile) {
		return nil, err
	}
	root := dir
	options := onkcompile.CompileOptions{}
	if cfg != nil {
		root = cfg.SchemaDir()
		options.AllowLegacyContracts = cfg.AllowLegacyContracts
	}
	pkg, err := CompileWithOptions(root, options)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		applyRoutePrefix(pkg, cfg.RoutePrefix)
	}
	if opts.ErrorRate < 0 || opts.ErrorRate > 1 {
		return nil, fmt.Errorf("error-rate %v must be within [0,1]", opts.ErrorRate)
	}
	server := &MockServer{
		mux: http.NewServeMux(),
		// Weak RNG is intentional: fixtures must be reproducible from the
		// configured seed, and this stream never guards secrets.
		//nolint:gosec // Deterministic seeded randomness is the feature.
		rng:       rand.New(rand.NewPCG(uint64(opts.Seed), ^uint64(opts.Seed))),
		errorRate: opts.ErrorRate,
		latency:   opts.Latency,
	}
	for _, file := range pkg.Files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if method.IsWebSocket() {
					wsPath, _ := method.WebSocketPath()
					pattern := "GET " + service.BasePath + wsPath
					server.mux.HandleFunc(pattern, server.handleWebSocket(method))
					server.routes++
					continue
				}
				verb, _ := method.Verb()
				path, _ := method.Path()
				pattern := strings.ToUpper(verb) + " " + service.BasePath + path
				server.mux.HandleFunc(pattern, server.handle(method))
				server.routes++
			}
		}
	}
	if server.routes == 0 {
		return nil, fmt.Errorf("no routes found under %s", root)
	}
	return server, nil
}

// Routes reports how many RPCs were registered.
func (m *MockServer) Routes() int { return m.routes }

// Handler exposes the underlying handler for tests and embedding.
func (m *MockServer) Handler() http.Handler { return m.mux }

// Run serves until ctx is cancelled or the listener fails.
func (m *MockServer) Run(ctx context.Context, addr string, out io.Writer) error {
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	httpServer := &http.Server{Handler: m.mux, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(listener) }()
	if out != nil {
		_, _ = fmt.Fprintf(out, "onekit mock: serving %d route(s) on http://%s\n", m.routes, listener.Addr())
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (m *MockServer) handle(method *onkir.Method) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.latency > 0 {
			time.Sleep(time.Duration(m.rng.Int64N(int64(m.latency) + 1)))
		}
		injectError := m.errorRate > 0 && m.rng.Float64() < m.errorRate && len(method.ErrorTypes) > 0

		w.Header().Set("Content-Type", "application/json")
		if method.IsStream() {
			m.stream(w, r, method, injectError)
			return
		}
		if injectError {
			errType := method.ErrorTypes[0]
			w.WriteHeader(mockStatus(errType))
			body, _ := json.Marshal(mockMessage(errType, 0))
			_, _ = w.Write(body)
			return
		}
		body, err := json.Marshal(mockMessage(method.Response, 0))
		if err != nil {
			http.Error(w, `{"message":"fixture generation failed"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}
}

// stream emits three SSE events then closes, mirroring the frame format of
// the generated servers ("data: <json>\n\n", with "event: error" frames for
// injected failures).
func (m *MockServer) stream(w http.ResponseWriter, r *http.Request, method *onkir.Method, injectError bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"message":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for i := range 3 {
		if r.Context().Err() != nil {
			return
		}
		var payload any
		eventName := ""
		if injectError && i == 2 {
			payload = map[string]string{"message": "injected failure"}
			eventName = "event: error\n"
		} else {
			payload = mockMessage(method.Response, 0)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "%sdata: %s\n\n", eventName, data)
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
	}
}

// handleWebSocket accepts the upgrade, streams three fixture frames, then
// holds the connection open discarding inbound frames until the peer closes.
func (m *MockServer) handleWebSocket(method *onkir.Method) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		go func() { <-ctx.Done(); _ = conn.CloseNow() }()
		for range 3 {
			body, err := json.Marshal(mockMessage(method.Response, 0))
			if err != nil {
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}
}

func mockStatus(message *onkir.Message) int {
	if code, ok := message.StatusCode(); ok {
		return code
	}
	return 500
}

// --- fixture generation -------------------------------------------------
//
// Everything below is a pure function of the compiled schema: no clocks, no
// randomness. Identical schemas therefore produce byte-identical responses,
// which keeps frontend snapshot tests stable across runs and machines.

func mockMessage(message *onkir.Message, depth int) any {
	if message == nil || depth > 4 {
		return map[string]any{}
	}
	if field := rootUnwrapMessageField(message); field != nil {
		value := mockFieldValue(field, depth)
		if field.Repeated {
			return []any{value}
		}
		return value
	}
	object := map[string]any{}
	for _, field := range message.Fields {
		object[field.Name] = mockFieldValue(field, depth)
	}
	return object
}

// mockOneofDiscriminatorKey mirrors the generated backends' discriminator
// fallback spelling.
const mockOneofFallbackDiscriminator = "type"

func mockFieldValue(field *onkir.Field, depth int) any {
	if field.Oneof != nil && len(field.Oneof.Variants) > 0 {
		variant := field.Oneof.Variants[0]
		discriminator := mockOneofFallbackDiscriminator
		if name, ok := field.Oneof.Discriminator(); ok && name != "" {
			discriminator = name
		}
		object := map[string]any{discriminator: variant.Tag()}
		if variant.Type != nil && variant.Type.Kind == onkir.KindMessage {
			if inner, okMap := mockMessage(variant.Type.Message, depth+1).(map[string]any); okMap {
				for name, value := range inner {
					object[name] = value
				}
			}
		} else if variant.Type != nil {
			object["value"] = mockType(variant.Type, depth+1)
		}
		return object
	}
	if d, ok := field.Decorator("empty"); ok {
		if value, okValue := d.Value(); okValue && value == "omit" {
			return nil
		}
	}
	if field.Repeated {
		items := make([]any, 0, 2)
		for range mockRepeatCount(field) {
			items = append(items, mockSingleValue(field, depth+1))
		}
		return items
	}
	if prefix, ok := flattenPrefixOf(field); ok {
		_ = prefix // flattened children are merged by their own fields below
		if field.Type != nil && field.Type.Kind == onkir.KindMessage {
			return mockMessage(field.Type.Message, depth+1)
		}
	}
	return mockSingleValue(field, depth+1)
}

func mockRepeatCount(field *onkir.Field) int {
	count := 1
	if d, ok := field.Decorator("min_items"); ok {
		if raw, okArg := d.Arg(0); okArg {
			if minimum, err := strconv.Atoi(raw); err == nil && minimum > count {
				count = minimum
			}
		}
	}
	if d, ok := field.Decorator("max_items"); ok {
		if raw, okArg := d.Arg(0); okArg {
			if maximum, err := strconv.Atoi(raw); err == nil && maximum < count {
				count = maximum
			}
		}
	}
	return count
}

// mockSingleValue renders one item honoring field-level @encode choices and
// validator-derived overrides before falling back to bare type defaults.
func mockSingleValue(field *onkir.Field, depth int) any {
	switch {
	case field.Type == nil:
		return nil
	case field.Type.Kind == onkir.KindEnum:
		if encodeValueOf(field) == "number" {
			return 0
		}
		return firstEnumJSONNameOf(field.Type.Enum)
	case isInt64KindOf(field.Type.Scalar) && field.Type.Kind == onkir.KindScalar:
		if encodeValueOf(field) == "number" {
			return 1729
		}
		return "1729"
	case field.Type.Kind == onkir.KindScalar && field.Type.Scalar == onkir.ScalarTimestamp:
		switch encodeValueOf(field) {
		case "unix_seconds":
			return 1735689600
		case "unix_millis":
			return 1735689600000
		case "date":
			return "2025-01-01"
		}
		return "2025-01-01T00:00:00Z"
	case field.Type.Kind == onkir.KindScalar && field.Type.Scalar == onkir.ScalarBytes:
		switch encodeValueOf(field) {
		case "hex":
			return "61"
		case "base64url", "base64url_raw", "base64_raw":
			return "YQ"
		}
		return "YQ=="
	case field.Type.Kind == onkir.KindScalar && field.Type.Scalar == onkir.ScalarString:
		return mockConstrainedString(field)
	}
	return mockType(field.Type, depth)
}

// mockConstrainedString derives validator-satisfying strings so fixtures
// pass client-side zod schemas unchanged.
func mockConstrainedString(field *onkir.Field) string {
	if d, ok := field.Decorator("in"); ok {
		value, _ := d.Arg(0)
		return value
	}
	switch {
	case field.HasDecorator("email"):
		return "user@example.com"
	case field.HasDecorator("uuid"):
		return "0f9ad6e5-8c1a-4b2e-9d3f-5a7c8e1b2d4f"
	case field.HasDecorator("uri"):
		return "https://example.com/resource"
	case field.HasDecorator("pattern"):
		return "match"
	}
	if d, ok := field.Decorator("len"); ok {
		minimum := 1
		if raw, okArg := d.Arg(0); okArg {
			if parsed, err := strconv.Atoi(raw); err == nil {
				minimum = parsed
			}
		}
		if minimum < 1 {
			minimum = 1
		}
		if minimum > 16 {
			minimum = 16
		}
		return strings.Repeat("x", minimum)
	}
	return "string"
}

// mockType renders a bare TypeRef fixture (map values, nested containers).
func mockType(t *onkir.Type, depth int) any {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case onkir.KindMessage:
		return mockMessage(t.Message, depth)
	case onkir.KindEnum:
		return firstEnumJSONNameOf(t.Enum)
	case onkir.KindMap:
		return map[string]any{"key": mockType(t.MapValue, depth+1)}
	case onkir.KindScalar:
		return mockScalar(t.Scalar)
	default:
		return nil
	}
}

func mockScalar(kind onkir.ScalarKind) any {
	switch kind {
	case onkir.ScalarString:
		return "string"
	case onkir.ScalarBool:
		return true
	case onkir.ScalarInt32:
		return 42
	case onkir.ScalarUint32:
		return 7
	case onkir.ScalarFloat32, onkir.ScalarFloat64:
		return 1.5
	case onkir.ScalarJSON:
		return map[string]any{}
	default:
		return nil
	}
}

func encodeValueOf(field *onkir.Field) string {
	d, ok := field.Decorator("encode")
	if !ok {
		return ""
	}
	value, _ := d.Value()
	return value
}

func isInt64KindOf(kind onkir.ScalarKind) bool {
	return kind == onkir.ScalarInt64 || kind == onkir.ScalarUint64
}

func firstEnumJSONNameOf(e *onkir.Enum) string {
	if len(e.Values) > 0 {
		return e.Values[0].JSONName()
	}
	return ""
}

func flattenPrefixOf(field *onkir.Field) (string, bool) {
	d, ok := field.Decorator("flatten")
	if !ok {
		return "", false
	}
	if prefix, okArg := d.NamedArg("prefix"); okArg {
		return prefix, true
	}
	return "", true
}

func rootUnwrapMessageField(message *onkir.Message) *onkir.Field {
	if message == nil || len(message.Fields) != 1 {
		return nil
	}
	field := message.Fields[0]
	if field.HasDecorator("unwrap") {
		return field
	}
	return nil
}
