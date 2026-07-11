package onkir

import "testing"

func TestParseScalarKindRoundTrip(t *testing.T) {
	kinds := []ScalarKind{
		ScalarString, ScalarBool, ScalarInt32, ScalarInt64,
		ScalarUint32, ScalarUint64, ScalarFloat32, ScalarFloat64,
		ScalarBytes, ScalarTimestamp,
	}
	for _, k := range kinds {
		got, ok := ParseScalarKind(k.String())
		if !ok || got != k {
			t.Fatalf("round trip failed for %v: got %v, ok=%v", k, got, ok)
		}
	}
	if _, ok := ParseScalarKind("nonsense"); ok {
		t.Fatalf("expected ParseScalarKind to fail for unknown name")
	}
}

func TestFindDecorator(t *testing.T) {
	decorators := []Decorator{
		{Name: "email"},
		{Name: "len", Args: []Arg{{Value: "2"}, {Value: "100"}}},
	}
	if _, ok := FindDecorator(decorators, "missing"); ok {
		t.Fatalf("expected missing decorator lookup to fail")
	}
	d, ok := FindDecorator(decorators, "len")
	if !ok || len(d.Args) != 2 {
		t.Fatalf("unexpected len decorator: %+v", d)
	}
	if !HasDecorator(decorators, "email") {
		t.Fatalf("expected HasDecorator(email) to be true")
	}
	if HasDecorator(decorators, "uuid") {
		t.Fatalf("expected HasDecorator(uuid) to be false")
	}
}

func TestDecoratorArgAccess(t *testing.T) {
	d := Decorator{Name: "flatten", Args: []Arg{{Name: "prefix", Value: "home_"}}}
	v, ok := d.NamedArg("prefix")
	if !ok || v != "home_" {
		t.Fatalf("unexpected named arg: %q, ok=%v", v, ok)
	}
	if _, ok := d.NamedArg("missing"); ok {
		t.Fatalf("expected missing named arg to fail")
	}
	positional := Decorator{Name: "format", Args: []Arg{{Value: "uuid"}}}
	v, ok = positional.Value()
	if !ok || v != "uuid" {
		t.Fatalf("unexpected positional value: %q, ok=%v", v, ok)
	}
	empty := Decorator{Name: "email"}
	if _, ok := empty.Value(); ok {
		t.Fatalf("expected empty decorator Value() to fail")
	}
}

func TestFieldDecoratorHelpers(t *testing.T) {
	f := &Field{
		Name: "name",
		Decorators: []Decorator{
			{Name: "len", Args: []Arg{{Value: "2"}, {Value: "100"}}},
		},
	}
	if !f.HasDecorator("len") {
		t.Fatalf("expected field to have len decorator")
	}
	if _, ok := f.Decorator("email"); ok {
		t.Fatalf("expected field to not have email decorator")
	}
}

func TestOneofHelpers(t *testing.T) {
	o := &Oneof{
		Args: []Arg{{Name: "discriminator", Value: "type"}, {Name: "flatten", Value: "true"}},
	}
	disc, ok := o.Discriminator()
	if !ok || disc != "type" {
		t.Fatalf("unexpected discriminator: %q, ok=%v", disc, ok)
	}
	if !o.Flatten() {
		t.Fatalf("expected Flatten() to be true")
	}

	notFlattened := &Oneof{Args: []Arg{{Name: "discriminator", Value: "type"}}}
	if notFlattened.Flatten() {
		t.Fatalf("expected Flatten() to be false when flatten arg absent")
	}

	variant := &OneofVariant{Name: "text", Decorators: []Decorator{{Name: "tag", Args: []Arg{{Value: "TEXT"}}}}}
	if variant.Tag() != "TEXT" {
		t.Fatalf("expected variant tag override, got %q", variant.Tag())
	}
	untagged := &OneofVariant{Name: "image"}
	if untagged.Tag() != "image" {
		t.Fatalf("expected variant tag to default to name, got %q", untagged.Tag())
	}
}

func TestHeaderHelpers(t *testing.T) {
	h := &Header{
		Name: "X-API-Key",
		Type: ScalarString,
		Decorators: []Decorator{
			{Name: "required"},
			{Name: "format", Args: []Arg{{Value: "uuid"}}},
			{Name: "auth", Args: []Arg{{Value: "api_key"}}},
			{Name: "auth_scheme_name", Args: []Arg{{Value: "ApiKeyAuth"}}},
			{Name: "example", Args: []Arg{{Value: "123e4567-e89b-12d3-a456-426614174000"}}},
			{Name: "deprecated", Args: []Arg{{Value: "use X-New-Key instead"}}},
		},
	}
	if !h.Required() {
		t.Fatalf("expected header to be required")
	}
	if v, ok := h.Format(); !ok || v != "uuid" {
		t.Fatalf("unexpected format: %q, ok=%v", v, ok)
	}
	if v, ok := h.AuthType(); !ok || v != "api_key" {
		t.Fatalf("unexpected auth type: %q, ok=%v", v, ok)
	}
	if v, ok := h.AuthSchemeName(); !ok || v != "ApiKeyAuth" {
		t.Fatalf("unexpected auth scheme name: %q, ok=%v", v, ok)
	}
	if v, ok := h.Example(); !ok || v != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected example: %q, ok=%v", v, ok)
	}
	if v, ok := h.Deprecated(); !ok || v != "use X-New-Key instead" {
		t.Fatalf("unexpected deprecated reason: %q, ok=%v", v, ok)
	}

	plain := &Header{Name: "X-Trace-Id", Type: ScalarString}
	if plain.Required() {
		t.Fatalf("expected plain header to not be required")
	}
	if _, ok := plain.Format(); ok {
		t.Fatalf("expected plain header to have no format")
	}
}

func TestMethodHelpers(t *testing.T) {
	m := &Method{
		Name: "getUser",
		Decorators: []Decorator{
			{Name: "get", Args: []Arg{{Value: "/users/{id}"}}},
		},
	}
	verb, ok := m.Verb()
	if !ok || verb != "get" {
		t.Fatalf("unexpected verb: %q, ok=%v", verb, ok)
	}
	path, ok := m.Path()
	if !ok || path != "/users/{id}" {
		t.Fatalf("unexpected path: %q, ok=%v", path, ok)
	}
	if m.IsStream() {
		t.Fatalf("expected IsStream() to be false")
	}
	if _, ok := m.BodyField(); ok {
		t.Fatalf("expected no body field")
	}

	stream := &Method{
		Decorators: []Decorator{
			{Name: "get", Args: []Arg{{Value: "/events"}}},
			{Name: "stream"},
		},
	}
	if !stream.IsStream() {
		t.Fatalf("expected IsStream() to be true")
	}

	withBody := &Method{
		Decorators: []Decorator{
			{Name: "put", Args: []Arg{{Value: "/users/{id}/profile"}}},
			{Name: "body", Args: []Arg{{Value: "profile"}}},
		},
	}
	body, ok := withBody.BodyField()
	if !ok || body != "profile" {
		t.Fatalf("unexpected body field: %q, ok=%v", body, ok)
	}

	noVerb := &Method{}
	if _, ok := noVerb.Verb(); ok {
		t.Fatalf("expected no verb to be found")
	}
	if _, ok := noVerb.Path(); ok {
		t.Fatalf("expected no path when verb is absent")
	}
}

func TestMessageIsErrorAndStatusCode(t *testing.T) {
	errMsg := &Message{
		Name:       "NotFoundError",
		Decorators: []Decorator{{Name: "status", Args: []Arg{{Value: "404"}}}},
	}
	if !errMsg.IsError() {
		t.Fatalf("expected NotFoundError to be recognized as an error message")
	}
	code, ok := errMsg.StatusCode()
	if !ok || code != 404 {
		t.Fatalf("unexpected status code: %d, ok=%v", code, ok)
	}

	plain := &Message{Name: "User"}
	if plain.IsError() {
		t.Fatalf("expected User to not be an error message")
	}
	if _, ok := plain.StatusCode(); ok {
		t.Fatalf("expected plain message to have no status code")
	}

	badStatus := &Message{
		Name:       "BadError",
		Decorators: []Decorator{{Name: "status", Args: []Arg{{Value: "not-a-number"}}}},
	}
	if _, ok := badStatus.StatusCode(); ok {
		t.Fatalf("expected malformed status code to fail gracefully")
	}
}

func TestMessageFullName(t *testing.T) {
	file := &File{Package: "examples.simpleapi.models"}
	parent := &Message{Name: "Outer", File: file}
	child := &Message{Name: "Inner", File: file, Parent: parent}
	if got := child.FullName(); got != "examples.simpleapi.models.Outer.Inner" {
		t.Fatalf("unexpected nested full name: %q", got)
	}
	if got := parent.FullName(); got != "examples.simpleapi.models.Outer" {
		t.Fatalf("unexpected top-level full name: %q", got)
	}

	noPackage := &Message{Name: "Standalone"}
	if got := noPackage.FullName(); got != "Standalone" {
		t.Fatalf("unexpected full name with no package: %q", got)
	}
}

func TestEnumFullNameAndJSONName(t *testing.T) {
	file := &File{Package: "examples.simpleapi.models"}
	parentMsg := &Message{Name: "Outer", File: file}
	e := &Enum{Name: "Status", File: file, Parent: parentMsg}
	if got := e.FullName(); got != "examples.simpleapi.models.Outer.Status" {
		t.Fatalf("unexpected enum full name: %q", got)
	}

	value := &EnumValue{Name: "ACTIVE", Decorators: []Decorator{{Name: "json", Args: []Arg{{Value: "active"}}}}}
	if got := value.JSONName(); got != "active" {
		t.Fatalf("unexpected enum value JSON name: %q", got)
	}
	plainValue := &EnumValue{Name: "INACTIVE"}
	if got := plainValue.JSONName(); got != "INACTIVE" {
		t.Fatalf("unexpected default enum value JSON name: %q", got)
	}
}
