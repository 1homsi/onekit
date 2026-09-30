package gendart

import _ "embed"

func GenerateRuntime() []byte { return []byte(runtimeSource) }

//go:embed runtime/onekit.dart
var runtimeSource string
