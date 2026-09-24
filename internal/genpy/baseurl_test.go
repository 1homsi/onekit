package genpy

import (
	"strings"
	"testing"
)

func TestPythonClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	out := string(GenerateClient(compileFixture(t), "models"))
	if !strings.Contains(out, `self.base_url = base_url.rstrip("/")`) {
		t.Fatalf("client does not trim base_url:\n%s", out)
	}
}
