package pyclientgen

import (
	"net/http"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/corezio/onekit/internal/annotations"
)

// writeServiceClient emits a single client class for a proto service, including
// the typed *ClientOptions and *CallOptions dataclasses, header surface, and
// per-RPC methods. SSE methods (stream=true) return an Iterator of event messages.
func writeServiceClient(p printer, service *protogen.Service) error {
	serviceName := string(service.Desc.Name())

	writeClientOptionsClass(p, service, serviceName)
	writeCallOptionsClass(p, service, serviceName)
	return writeClientClass(p, service, serviceName)
}

func writeClientOptionsClass(p printer, service *protogen.Service, serviceName string) {
	p("@dataclass")
	p("class %sClientOptions:", serviceName)
	p(`    """Construct-time options for %sClient."""`, serviceName)
	p("    transport: Optional[HttpTransport] = None")
	p("    default_headers: Optional[Mapping[str, str]] = None")
	p("    timeout: Optional[float] = None")
	p(`    content_type: str = "application/json"`)
	p("    # Total attempts including the first (1 = no retries). Transport errors and")
	p("    # HTTP 429/502/503/504 are retried with exponential backoff. SSE never retries.")
	p("    max_retry_attempts: int = 1")
	p("    # Delay in seconds before the first retry; doubles on each subsequent retry.")
	p("    retry_backoff: float = 0.25")

	// Typed kwargs for every service-level header annotation.
	serviceHeaders := annotations.GetServiceHeaders(service)
	for _, header := range serviceHeaders {
		p("    %s: Optional[str] = None", headerOptionName(header.GetName()))
	}
	p("")
	p("")
}

func writeCallOptionsClass(p printer, service *protogen.Service, serviceName string) {
	p("@dataclass")
	p("class %sCallOptions:", serviceName)
	p(`    """Per-call options for %sClient methods."""`, serviceName)
	p("    headers: Optional[Mapping[str, str]] = None")
	p("    timeout: Optional[float] = None")
	p("    content_type: Optional[str] = None")

	// Method-level headers and service-level headers are both available per-call;
	// service headers are also on the client options. Dedup against service set.
	seen := make(map[string]bool)
	for _, header := range annotations.GetServiceHeaders(service) {
		seen[header.GetName()] = true
		p("    %s: Optional[str] = None", headerOptionName(header.GetName()))
	}
	for _, method := range service.Methods {
		for _, header := range annotations.GetMethodHeaders(method) {
			if seen[header.GetName()] {
				continue
			}
			seen[header.GetName()] = true
			p("    %s: Optional[str] = None", headerOptionName(header.GetName()))
		}
	}
	p("")
	p("")
}

func writeClientClass(p printer, service *protogen.Service, serviceName string) error {
	p("class %sClient:", serviceName)
	p(`    """Generated client for %s."""`, service.Desc.FullName())

	writeClientConstructor(p, service, serviceName)

	for _, method := range service.Methods {
		if err := writeRPCMethod(p, service, method, serviceName); err != nil {
			return err
		}
	}

	writeErrorHandler(p)
	p("")
	return nil
}

func writeClientConstructor(p printer, service *protogen.Service, serviceName string) {
	p("    def __init__(")
	p("        self,")
	p("        base_url: str,")
	p("        options: Optional[%sClientOptions] = None,", serviceName)
	p("    ) -> None:")
	p(`        self._base_url = base_url.rstrip("/")`)
	p("        opts = options or %sClientOptions()", serviceName)
	p("        self._transport: HttpTransport = opts.transport or UrllibTransport()")
	p("        self._default_headers: dict[str, str] = dict(opts.default_headers or {})")
	p("        self._timeout = opts.timeout")
	p("        self._content_type = opts.content_type")
	p("        self._max_retry_attempts = opts.max_retry_attempts")
	p("        self._retry_backoff = opts.retry_backoff")

	// Apply typed service-header options onto default headers.
	for _, header := range annotations.GetServiceHeaders(service) {
		propName := headerOptionName(header.GetName())
		p("        if opts.%s is not None:", propName)
		p(`            self._default_headers["%s"] = opts.%s`, header.GetName(), propName)
	}

	p("")
}

func writeRPCMethod(p printer, service *protogen.Service, method *protogen.Method, serviceName string) error {
	cfg := buildMethodConfig(service, method)

	bodyField, err := annotations.GetBodyField(method)
	if err != nil && !annotations.IsNoBodyField(err) {
		return err
	}
	if bodyField != nil {
		cfg.bodyFieldPyName = escapePyKeyword(string(bodyField.Desc.Name()))
	}

	if cfg.isSSE {
		writeSSEMethod(p, service, method, serviceName, cfg)
		return nil
	}

	pyMethodName := snakeCase(string(method.Desc.Name()))
	inputType := pythonTypeName(method.Input)
	outputType := resolveOutputType(method)

	p("    def %s(", pyMethodName)
	p("        self,")
	p("        req: %s,", inputType)
	p("        options: Optional[%sCallOptions] = None,", serviceName)
	p("    ) -> %s:", outputType)
	p(`        """Calls %s."""`, method.Desc.FullName())
	p("        opts = options or %sCallOptions()", serviceName)
	p(`        content_type = opts.content_type or self._content_type`)
	p(`        if content_type != "application/json":`)
	p(`            raise NotImplementedError("only application/json is implemented")`)

	writePathBuilding(p, cfg)
	writeQueryBuilding(p, cfg)
	writeHeaderBuilding(p, service, method, cfg)
	writeBodyBuilding(p, cfg)
	writeTransportCall(p, cfg)
	writeResponseParsing(p, method)
	p("")
	return nil
}

// writeSSEMethod emits a generator method that opens an SSE connection through the
// transport's stream() extension and yields one decoded event message per SSE frame.
func writeSSEMethod(
	p printer,
	service *protogen.Service,
	method *protogen.Method,
	serviceName string,
	cfg *methodConfig,
) {
	pyMethodName := snakeCase(string(method.Desc.Name()))
	inputType := pythonTypeName(method.Input)
	outputType := resolveOutputType(method)

	p("    def %s(", pyMethodName)
	p("        self,")
	p("        req: %s,", inputType)
	p("        options: Optional[%sCallOptions] = None,", serviceName)
	p("    ) -> Iterator[%s]:", outputType)
	p(`        """Calls %s (Server-Sent Events stream)."""`, method.Desc.FullName())
	p("        opts = options or %sCallOptions()", serviceName)

	writePathBuilding(p, cfg)
	writeQueryBuilding(p, cfg)
	writeSSEHeaderBuilding(p, service, method, cfg)
	writeBodyBuilding(p, cfg)

	p(`        stream = getattr(self._transport, "stream", None)`)
	p("        if stream is None:")
	p("            raise TypeError(")
	p(`                "transport does not support SSE streaming; "`)
	p(`                "provide a stream() method (see UrllibTransport.stream)"`)
	p("            )")
	p("        conn: SseConnection = stream(")
	p(`            method="%s",`, cfg.httpMethod)
	p("            url=self._base_url + path,")
	p("            headers=headers,")
	p("            body=body,")
	p("            timeout=opts.timeout if opts.timeout is not None else self._timeout,")
	p("        )")
	p("        if conn.status >= 400:")
	p("            error_body = conn.read_body()")
	p("            conn.close()")
	p("            self._raise_for_status(HttpResponse(")
	p("                status=conn.status,")
	p("                headers=conn.headers,")
	p("                body=error_body,")
	p("            ))")
	p("        try:")
	p("            for data in _iter_sse_data(conn.iter_lines()):")
	p("                yield %s.from_dict(json.loads(data))", outputType)
	p("        finally:")
	p("            conn.close()")
	p("")
}

// writeSSEHeaderBuilding mirrors writeHeaderBuilding but negotiates an SSE response.
// Content-Type is only sent when the request carries a body.
func writeSSEHeaderBuilding(p printer, service *protogen.Service, method *protogen.Method, cfg *methodConfig) {
	p("        headers: dict[str, str] = dict(self._default_headers)")
	if cfg.hasBody {
		p(`        headers["Content-Type"] = "application/json"`)
	}
	p(`        headers["Accept"] = "text/event-stream"`)
	p("        if opts.headers:")
	p("            headers.update(opts.headers)")

	for _, header := range annotations.GetServiceHeaders(service) {
		propName := headerOptionName(header.GetName())
		p("        if opts.%s is not None:", propName)
		p(`            headers["%s"] = opts.%s`, header.GetName(), propName)
	}
	for _, header := range annotations.GetMethodHeaders(method) {
		propName := headerOptionName(header.GetName())
		p("        if opts.%s is not None:", propName)
		p(`            headers["%s"] = opts.%s`, header.GetName(), propName)
	}
}

func writePathBuilding(p printer, cfg *methodConfig) {
	p(`        path = "%s"`, cfg.fullPath)
	for _, param := range cfg.pathParams {
		pyName := snakeCase(param)
		p(`        path = path.replace("{%s}", urllib.parse.quote(str(req.%s), safe=""))`, param, pyName)
	}
}

func writeQueryBuilding(p printer, cfg *methodConfig) {
	// With body field selection, non-body fields bind from path/query even on
	// POST/PUT/PATCH, so query encoding applies there too.
	useQuery := cfg.httpMethod == http.MethodGet || cfg.httpMethod == http.MethodDelete ||
		cfg.bodyFieldPyName != ""
	if !useQuery {
		return
	}
	if len(cfg.queryParams) == 0 {
		return
	}
	p("        query_pairs: list[tuple[str, str]] = []")
	for _, qp := range cfg.queryParams {
		writeQueryParamAppend(p, qp)
	}
	p("        if query_pairs:")
	p(`            path = path + "?" + urllib.parse.urlencode(query_pairs, doseq=True)`)
}

func writeQueryParamAppend(p printer, qp annotations.QueryParam) {
	pyField := escapePyKeyword(string(qp.Field.Desc.Name()))
	src := "req." + pyField
	if qp.Field.Desc.IsList() {
		p("        if %s:", src)
		p(`            for _v in %s:`, src)
		p(`                query_pairs.append(("%s", str(_v)))`, qp.ParamName)
		return
	}
	//nolint:exhaustive // default covers all numeric/message kinds with a single expression
	switch qp.Field.Desc.Kind() {
	case protoreflect.StringKind:
		p("        if %s:", src)
		p(`            query_pairs.append(("%s", str(%s)))`, qp.ParamName, src)
	case protoreflect.BoolKind:
		p("        if %s:", src)
		p(`            query_pairs.append(("%s", "true" if %s else "false"))`, qp.ParamName, src)
	default:
		p("        if %s is not None and %s != 0:", src, src)
		p(`            query_pairs.append(("%s", str(%s)))`, qp.ParamName, src)
	}
}

func writeHeaderBuilding(p printer, service *protogen.Service, method *protogen.Method, cfg *methodConfig) {
	p("        headers: dict[str, str] = dict(self._default_headers)")
	p(`        headers["Content-Type"] = content_type`)
	p(`        headers["Accept"] = "application/json"`)
	p("        if opts.headers:")
	p("            headers.update(opts.headers)")

	for _, header := range annotations.GetServiceHeaders(service) {
		propName := headerOptionName(header.GetName())
		p("        if opts.%s is not None:", propName)
		p(`            headers["%s"] = opts.%s`, header.GetName(), propName)
	}
	for _, header := range annotations.GetMethodHeaders(method) {
		propName := headerOptionName(header.GetName())
		p("        if opts.%s is not None:", propName)
		p(`            headers["%s"] = opts.%s`, header.GetName(), propName)
	}
	_ = cfg
}

func writeBodyBuilding(p printer, cfg *methodConfig) {
	if !cfg.hasBody {
		p("        body: Optional[bytes] = None")
		return
	}
	if cfg.bodyFieldPyName != "" {
		p("        _body_msg = req.%s", cfg.bodyFieldPyName)
		p(`        body = json.dumps(_body_msg.to_dict() if _body_msg is not None else {}).encode("utf-8")`)
		return
	}
	p(`        body = json.dumps(req.to_dict()).encode("utf-8")`)
}

func writeTransportCall(p printer, cfg *methodConfig) {
	p("        resp = self._request_with_retries(")
	p(`            method="%s",`, cfg.httpMethod)
	p(`            url=self._base_url + path,`)
	p("            headers=headers,")
	p("            body=body,")
	p(`            timeout=opts.timeout if opts.timeout is not None else self._timeout,`)
	p("        )")
}

func writeResponseParsing(p printer, method *protogen.Method) {
	outputType := resolveOutputType(method)
	p("        if resp.status >= 400:")
	p("            self._raise_for_status(resp)")
	p("        if not resp.body:")
	if outputType == pyNone {
		p("            return None")
	} else {
		p("            return %s()", outputType)
	}
	p(`        return %s.from_dict(json.loads(resp.body))`, outputType)
}

func writeErrorHandler(p printer) {
	p("    def _request_with_retries(")
	p("        self,")
	p("        method: str,")
	p("        url: str,")
	p("        headers: Mapping[str, str],")
	p("        body: Optional[bytes],")
	p("        timeout: Optional[float],")
	p("    ) -> HttpResponse:")
	p(`        """Executes a request, retrying transport errors and 429/502/503/504."""`)
	p("        attempts = max(1, self._max_retry_attempts)")
	p("        last_exc: Optional[Exception] = None")
	p("        for attempt in range(attempts):")
	p("            if attempt > 0:")
	p("                time.sleep(self._retry_backoff * (2 ** (attempt - 1)))")
	p("            try:")
	p("                resp = self._transport.request(")
	p("                    method=method, url=url, headers=headers, body=body, timeout=timeout,")
	p("                )")
	p("            except Exception as exc:  # noqa: BLE001 - transport errors vary by implementation")
	p("                last_exc = exc")
	p("                continue")
	p("            if attempt < attempts - 1 and resp.status in (429, 502, 503, 504):")
	p("                continue")
	p("            return resp")
	p("        assert last_exc is not None")
	p("        raise last_exc")
	p("")
	p("    def _raise_for_status(self, resp: HttpResponse) -> None:")
	p(`        """Map a non-2xx response to the most specific exception available."""`)
	p(`        body = resp.body or b""`)
	p("        parsed: Any = None")
	p(`        ctype = (resp.headers or {}).get("Content-Type", "")`)
	p(`        looks_jsonish = "json" in ctype.lower() or body[:1] in (b"{", b"[")`)
	p("        if looks_jsonish:")
	p("            try:")
	p(`                parsed = json.loads(body.decode("utf-8"))`)
	p("            except (ValueError, UnicodeDecodeError):")
	p("                parsed = None")
	p(`        if resp.status == 400 and isinstance(parsed, dict) and "violations" in parsed:`)
	p("            violations = [")
	p(`                FieldViolation(field=v.get("field", ""), description=v.get("description", ""))`)
	p(`                for v in parsed.get("violations", [])`)
	p("            ]")
	p("            raise ValidationError(resp.status, body, resp.headers, violations)")
	p("        if isinstance(parsed, dict):")
	p("            for err_cls, required_keys in _ERROR_CLASSES:")
	p("                if required_keys and required_keys.issubset(parsed.keys()):")
	p("                    raise err_cls.populate(resp.status, body, resp.headers, parsed)")
	p("        raise ApiError(resp.status, body, resp.headers)")
}

// resolveOutputType returns the Python class name for a method's response type.
// Root-unwrapped messages keep the wrapper class name (the wrapper still has
// to_dict / from_dict that return the unwrapped shape).
func resolveOutputType(method *protogen.Method) string {
	return pythonTypeName(method.Output)
}

// methodConfig captures every detail of an RPC method needed for generation.
type methodConfig struct {
	methodName      string
	httpMethod      string
	fullPath        string
	pathParams      []string
	queryParams     []annotations.QueryParam
	hasBody         bool
	isSSE           bool
	bodyFieldPyName string // non-empty when body: "<field>" selects a sub-message body
}

func buildMethodConfig(service *protogen.Service, method *protogen.Method) *methodConfig {
	methodName := string(method.Desc.Name())
	httpConfig := annotations.GetMethodHTTPConfig(method)

	httpMethod := http.MethodPost
	httpPath := "/" + annotations.LowerFirst(methodName)
	var pathParams []string

	if httpConfig != nil {
		if httpConfig.Method != "" {
			httpMethod = httpConfig.Method
		}
		if httpConfig.Path != "" {
			httpPath = httpConfig.Path
		}
		pathParams = httpConfig.PathParams
	}

	basePath := annotations.GetServiceBasePath(service)
	fullPath := annotations.BuildHTTPPath(basePath, httpPath)

	isSSE := httpConfig != nil && httpConfig.Stream
	hasBody := httpMethod == http.MethodPost || httpMethod == http.MethodPut || httpMethod == http.MethodPatch

	return &methodConfig{
		methodName:  methodName,
		httpMethod:  httpMethod,
		fullPath:    fullPath,
		pathParams:  pathParams,
		queryParams: annotations.GetQueryParams(method.Input),
		hasBody:     hasBody,
		isSSE:       isSSE,
	}
}

// snakeCaseExtraCapacity is the expected number of underscores inserted when
// converting CamelCase to snake_case. Pre-sizing the output avoids realloc on
// most identifiers.
const snakeCaseExtraCapacity = 4

// snakeCase converts CamelCase to snake_case. Adapted from PR #132 (@elzalem).
// protogen returns CamelCase by default and Python methods are conventionally
// snake_case.
func snakeCase(s string) string {
	out := make([]byte, 0, len(s)+snakeCaseExtraCapacity)
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && (s[i-1] < 'A' || s[i-1] > 'Z') {
				out = append(out, '_')
			}
			out = append(out, c+('a'-'A'))
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}

// headerOptionName converts an HTTP header name to a Python keyword argument.
// "X-API-Key" -> "api_key", "X-Request-ID" -> "request_id". The original header
// name is always preserved when writing the request.
func headerOptionName(headerName string) string {
	name := strings.TrimPrefix(headerName, "X-")
	name = strings.TrimPrefix(name, "x-")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ToLower(name)
	return escapePyKeyword(name)
}
