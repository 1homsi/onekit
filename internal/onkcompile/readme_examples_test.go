package onkcompile

import (
	"os"
	"strings"
	"testing"

	"github.com/1homsi/onekit/internal/onklang"
)

func TestReadmeOnkExamplesParse(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	parsed := 0
	for {
		open := strings.Index(text, "```onk\n")
		if open < 0 {
			break
		}
		text = text[open+len("```onk\n"):]
		end := strings.Index(text, "```")
		if end < 0 {
			break
		}
		block := text[:end]
		text = text[end:]
		if strings.Contains(block, "package ") || !startsDeclaration(block) {
			continue
		}
		if _, err := onklang.Parse("package readme\n" + block); err != nil {
			t.Errorf("README .onk example does not parse: %v\n%s", err, block)
		}
		parsed++
	}
	if parsed < 10 {
		t.Fatalf("only %d README examples were checked", parsed)
	}
}

func startsDeclaration(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		for _, keyword := range []string{"message ", "enum ", "service ", "import "} {
			if strings.HasPrefix(line, keyword) {
				return true
			}
		}
		return false
	}
	return false
}
