package gengo

import _ "embed"

func writeWSJSONRuntime(p *Printer) {
	p.P(wsJSONRuntimeSource)
}

//go:embed runtime/wsjson.go.tmpl
var wsJSONRuntimeSource string
