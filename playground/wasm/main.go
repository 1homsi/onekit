//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/1homsi/onekit/internal/playground"
)

func argument(args []js.Value) string {
	if len(args) == 0 {
		return ""
	}
	return args[0].String()
}

func main() {
	js.Global().Set("onekGenerate", js.FuncOf(func(_ js.Value, args []js.Value) any {
		out, err := json.Marshal(playground.Run(argument(args)))
		if err != nil {
			return `{"diagnostics":[{"message":"internal error"}],"files":[]}`
		}
		return string(out)
	}))
	js.Global().Set("onekFormat", js.FuncOf(func(_ js.Value, args []js.Value) any {
		formatted, problem := playground.Format(argument(args))
		out, err := json.Marshal(map[string]any{"formatted": formatted, "diagnostic": problem})
		if err != nil {
			return `{"formatted":"","diagnostic":{"message":"internal error"}}`
		}
		return string(out)
	}))
	select {}
}
