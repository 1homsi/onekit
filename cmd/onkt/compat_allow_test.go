package main

import (
	"testing"

	"github.com/1homsi/onekit/internal/onkcompat"
)

func TestWithoutAllowedMatchesPathsAndChildren(t *testing.T) {
	findings := []onkcompat.Finding{
		{Path: "app.User.email"},
		{Path: "app.User.name"},
		{Path: "app.UserList.items"},
		{Path: "app.API get /users"},
		{Path: "app.Other"},
	}
	kept := withoutAllowed(findings, []string{"app.User", "app.API"})
	if len(kept) != 2 || kept[0].Path != "app.UserList.items" || kept[1].Path != "app.Other" {
		t.Fatalf("kept = %+v", kept)
	}
}
