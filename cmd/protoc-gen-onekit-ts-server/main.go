package main

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/1homsi/onekit/internal/tsservergen"
)

func main() {
	options := protogen.Options{}

	options.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		gen := tsservergen.New(plugin)
		return gen.Generate()
	})
}
