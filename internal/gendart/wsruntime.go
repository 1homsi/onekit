package gendart

import _ "embed"

func GenerateWSRuntime() []byte    { return []byte(wsRuntimeSource) }
func GenerateWSConnectIO() []byte  { return []byte(wsConnectIOSource) }
func GenerateWSConnectWeb() []byte { return []byte(wsConnectWebSource) }

//go:embed runtime/onekit_ws_io.dart
var wsConnectIOSource string

//go:embed runtime/onekit_ws_web.dart
var wsConnectWebSource string

//go:embed runtime/onekit_ws.dart
var wsRuntimeSource string
