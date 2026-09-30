package genswift

import _ "embed"

func GenerateRuntime() []byte { return []byte(runtimeSource) }

//go:embed runtime/Onekit.swift
var runtimeSource string
