package gendart

import (
	"strings"

	"github.com/1homsi/onekit/internal/onkir"
)

func GenerateClient(file *onkir.File) []byte {
	return GenerateClientWithResolver(file, nil, "")
}

func GenerateClientWithResolver(file *onkir.File, resolver PackageResolver, runtimeDir string) []byte {
	if len(file.Services) == 0 {
		return nil
	}
	hasWS := onkir.FileHasWSMethods(file)
	body := newPrinter(resolver)
	if hasWS {
		writeWSCodecs(body, file)
	}
	for _, s := range file.Services {
		writeClientClass(body, s)
	}
	code := string(body.Bytes())
	p := newPrinter(resolver)
	writeFileHeader(p, code)
	var dartImports []string
	if strings.Contains(code, "jsonEncode(") || strings.Contains(code, "jsonDecode(") || strings.Contains(code, "utf8.") || strings.Contains(code, "base64.") {
		dartImports = append(dartImports, "dart:convert")
	}
	if strings.Contains(code, "Uint8List") {
		dartImports = append(dartImports, "dart:typed_data")
	}
	local := []string{"import '" + runtimeDir + "onekit.dart';"}
	if hasWS {
		local = append(local, "import '"+runtimeDir+"onekit_ws.dart';")
	}
	if usesLocalTypes(file, resolver) {
		local = append(local, "import 'models.dart';")
	}
	for _, ref := range collectServiceExternalRefs(file, resolver) {
		local = append(local, "import '"+ref.ImportPath+"' as "+ref.Alias+";")
	}
	writeImports(p, dartImports, []string{"import 'package:http/http.dart' as http;"}, local)
	p.P("export '", runtimeDir, "onekit.dart';")
	if hasWS {
		p.P("export '", runtimeDir, "onekit_ws.dart';")
	}
	p.P("export 'models.dart';")
	p.P()
	p.b.WriteString(code)
	return p.Bytes()
}

func usesLocalTypes(file *onkir.File, resolver PackageResolver) bool {
	local := func(m *onkir.Message) bool {
		if resolver == nil {
			return true
		}
		_, external := resolver.ResolveMessage(m)
		return !external
	}
	for _, s := range file.Services {
		for _, m := range s.Methods {
			if local(m.Request) || local(m.Response) {
				return true
			}
			for _, e := range m.ErrorTypes {
				if local(e) {
					return true
				}
			}
		}
	}
	return false
}

func serviceHasWS(s *onkir.Service) bool {
	for _, m := range s.Methods {
		if m.IsWebSocket() {
			return true
		}
	}
	return false
}

func writeClientClass(p *Printer, s *onkir.Service) {
	name := s.Name + "Client"
	ws := serviceHasWS(s)
	writeDoc(p, s.Doc)
	p.P("class ", name, " {")
	p.Indent()
	params := []string{
		"http.Client? httpClient",
		"Map<String, String>? headers",
		"this.timeout = defaultRequestTimeout",
		"this.maxResponseBodyBytes = defaultMaxResponseBodyBytes",
		"this.maxSseLineBytes = defaultMaxSseLineBytes",
	}
	if ws {
		params = append(params,
			"this.maxWsFrameBytes = defaultMaxWsFrameBytes",
			"this.maxWsMessageBytes = defaultMaxWsMessageBytes",
			"this.wsPingInterval = defaultWsPingInterval",
		)
	}
	p.P(name, "(")
	p.Indent()
	p.P("String baseUrl, {")
	p.Indent()
	for _, param := range params {
		p.P(param, ",")
	}
	p.Dedent()
	p.P("})  : baseUrl = onekitTrimSlash(baseUrl),")
	p.Indent()
	p.P("headers = headers ?? {},")
	p.P("_http = httpClient ?? http.Client();")
	p.Dedent()
	p.Dedent()
	p.P()
	p.P("final String baseUrl;")
	p.P("final Map<String, String> headers;")
	p.P("final Duration? timeout;")
	p.P("final int maxResponseBodyBytes;")
	p.P("final int maxSseLineBytes;")
	if ws {
		p.P("final int maxWsFrameBytes;")
		p.P("final int maxWsMessageBytes;")
		p.P("final Duration? wsPingInterval;")
	}
	p.P("final http.Client _http;")
	p.P()
	p.P("void close() => _http.close();")
	p.P()
	for _, m := range s.Methods {
		switch {
		case m.IsWebSocket():
			writeWSClientMethod(p, s, m)
		case m.IsStream():
			writeSSEClientMethod(p, s, m)
		default:
			writeClientMethod(p, s, m)
		}
	}
	p.Dedent()
	p.P("}")
	p.P()
}

const callOptions = "{Map<String, String>? headers, Duration? timeout}"

func writeMethodHeader(p *Printer, m *onkir.Method) {
	writeDoc(p, m.Doc)
	writeDeprecated(p, m.Deprecated)
}

func writePathAndQuery(p *Printer, route string, req *onkir.Message, withQuery bool) {
	p.P("var path = ", dartString(route), ";")
	for _, name := range onkir.PathParamNames(route) {
		field := onkir.FindField(req, name)
		if field == nil {
			continue
		}
		p.P("path = path.replaceAll(", dartString("{"+name+"}"), ", onekitPathValue(req.", Ident(field.Name), "));")
	}
	p.P("final query = <MapEntry<String, String>>[];")
	if !withQuery {
		return
	}
	for _, field := range req.Fields {
		d, ok := field.Decorator("query")
		if !ok || field.Type == nil || field.Type.Kind != onkir.KindScalar {
			continue
		}
		queryName, _ := d.Value()
		if queryName == "" {
			queryName = field.Name
		}
		id := "req." + Ident(field.Name)
		key := dartString(queryName)
		switch {
		case field.Repeated:
			p.P("for (final value in ", id, ") {")
			p.Indent()
			p.P("query.add(MapEntry(", key, ", value.toString()));")
			p.Dedent()
			p.P("}")
		case isNullableKind(field):
			p.P("final ", Ident(field.Name), "Query = ", id, ";")
			p.P("if (", Ident(field.Name), "Query != null) query.add(MapEntry(", key, ", ", Ident(field.Name), "Query.toString()));")
		default:
			p.P("query.add(MapEntry(", key, ", ", id, ".toString()));")
		}
	}
}

func writeTypedErrors(p *Printer, m *onkir.Method, status, body string) {
	for _, errType := range m.ErrorTypes {
		code := 500
		if c, ok := errType.StatusCode(); ok {
			code = c
		}
		p.P("if (", status, " == ", code, ") {")
		p.Indent()
		p.P("final payload = onekitTryJson(", body, ");")
		p.P("if (payload is Map) throw ", p.MessageTypeName(errType), ".fromJson(payload);")
		p.Dedent()
		p.P("}")
	}
}

func (p *Printer) bodyExpr(m *onkir.Method) string {
	bodyField, ok := m.BodyField()
	if !ok {
		return "jsonEncode(req.toJson())"
	}
	field := onkir.FindField(m.Request, bodyField)
	if field == nil {
		return "jsonEncode(req.toJson()[" + dartString(bodyField) + "])"
	}
	id := "req." + Ident(field.Name)
	if field.Oneof != nil {
		return "jsonEncode(" + id + "?.toJson())"
	}
	if isNullableKind(field) {
		return "jsonEncode(" + id + " == null ? null : " + p.encodeValue(field.Type, field, id+"!", false) + ")"
	}
	return "jsonEncode(" + p.fieldWireValue(field, id) + ")"
}

func writeClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	route, _ := m.Path()
	bodyBearing := onkir.IsBodyBearingVerb(verb)
	writeMethodHeader(p, m)
	p.P("Future<", p.MessageTypeName(m.Response), "> ", MethodIdent(m.Name), "(", p.MessageTypeName(m.Request), " req, ", callOptions, ") async {")
	p.Indent()
	p.P("onekitCheck(req.validate());")
	writePathAndQuery(p, s.BasePath+route, m.Request, !bodyBearing)
	p.P("final request = http.Request(", dartString(strings.ToUpper(verb)), ", onekitUri(baseUrl, path, query))")
	p.Indent()
	p.P("..headers.addAll(onekitHeaders(this.headers, headers));")
	p.Dedent()
	if bodyBearing {
		p.P("request.headers['content-type'] = 'application/json';")
		p.P("request.body = ", p.bodyExpr(m), ";")
	}
	p.P("final response = await onekitSend(_http, request, onekitTimeout(this.timeout, timeout), maxResponseBodyBytes);")
	p.P("if (response.ok) return ", p.MessageTypeName(m.Response), ".fromJson(onekitDecodeBody(response.body));")
	writeTypedErrors(p, m, "response.status", "response.body")
	p.P("throw UnexpectedStatusException(response.status, response.body, response.headers);")
	p.Dedent()
	p.P("}")
	p.P()
}

func writeSSEClientMethod(p *Printer, s *onkir.Service, m *onkir.Method) {
	verb, _ := m.Verb()
	route, _ := m.Path()
	writeMethodHeader(p, m)
	p.P("Stream<", p.MessageTypeName(m.Response), "> ", MethodIdent(m.Name), "(", p.MessageTypeName(m.Request), " req, {Map<String, String>? headers}) async* {")
	p.Indent()
	p.P("onekitCheck(req.validate());")
	writePathAndQuery(p, s.BasePath+route, m.Request, true)
	p.P("final request = http.Request(", dartString(strings.ToUpper(verb)), ", onekitUri(baseUrl, path, query))")
	p.Indent()
	p.P("..headers.addAll(onekitHeaders(this.headers, headers))")
	p.P("..headers['accept'] = 'text/event-stream';")
	p.Dedent()
	p.P("final response = await _http.send(request);")
	p.P("if (response.statusCode < 200 || response.statusCode >= 300) {")
	p.Indent()
	p.P("final body = await onekitReadBounded(response.stream, maxResponseBodyBytes);")
	writeTypedErrors(p, m, "response.statusCode", "body")
	p.P("throw UnexpectedStatusException(response.statusCode, body, response.headers);")
	p.Dedent()
	p.P("}")
	p.P("await for (final event in onekitSseEvents(response.stream, maxSseLineBytes)) {")
	p.Indent()
	p.P("final payload = jsonDecode(event.data);")
	p.P("if (event.event == 'error') throw StreamException(payload);")
	p.P("yield ", p.MessageTypeName(m.Response), ".fromJson(payload);")
	p.Dedent()
	p.P("}")
	p.Dedent()
	p.P("}")
	p.P()
}
