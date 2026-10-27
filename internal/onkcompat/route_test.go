package onkcompat

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompile"
)

func TestCompareReportsWhichRoutePartChanged(t *testing.T) {
	base := `namespace app;

model Req {
  @query
  id: string;
}

model Res {}

model Other {}

@error
model NotFound {
  @statusCode _: 404;
}
`
	old := compile(t, base+`
interface API {
  @get
  @route("/items")
  get(...Req): Res;
}
`)
	newer := compile(t, base+`
interface API {
  @get
  @route("/items")
  get(...Req): Other | NotFound;
}
`)
	oldPkg, err := onkcompile.Compile([]onkcompile.Source{*old})
	if err != nil {
		t.Fatal(err)
	}
	newPkg, err := onkcompile.Compile([]onkcompile.Source{*newer})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Finding{}
	for _, f := range Compare(oldPkg, newPkg) {
		got[f.Message] = f
	}
	response, ok := got["response message changed"]
	if !ok || response.Before != "app.Res" || response.After != "app.Other" {
		t.Fatalf("response finding = %+v, all = %+v", response, got)
	}
	errs, ok := got["error responses changed"]
	if !ok || errs.Before != "" || errs.After != "app.NotFound:404" {
		t.Fatalf("error finding = %+v", errs)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected findings: %+v", got)
	}
}
