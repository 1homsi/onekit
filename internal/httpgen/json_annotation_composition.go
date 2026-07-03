package httpgen

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

type directJSONEncodingFeatureSet struct {
	message  *protogen.Message
	features []string
}

func validateDirectJSONEncodingComposition(file *protogen.File) error {
	featuresByMessage := map[string]*directJSONEncodingFeatureSet{}

	addFeature := func(message *protogen.Message, feature string) {
		fullName := string(message.Desc.FullName())
		set := featuresByMessage[fullName]
		if set == nil {
			set = &directJSONEncodingFeatureSet{message: message}
			featuresByMessage[fullName] = set
		}
		set.features = append(set.features, feature)
	}

	for _, ctx := range collectInt64EncodingContext(file) {
		addFeature(ctx.Message, "int64_encoding=NUMBER")
	}
	for _, ctx := range collectNullableContext(file) {
		addFeature(ctx.Message, "nullable")
	}
	for _, ctx := range collectEmptyBehaviorContext(file) {
		addFeature(ctx.Message, "empty_behavior")
	}
	for _, ctx := range collectTimestampFormatContext(file) {
		addFeature(ctx.Message, "timestamp_format")
	}
	for _, ctx := range collectBytesEncodingContext(file) {
		addFeature(ctx.Message, "bytes_encoding")
	}

	for _, set := range featuresByMessage {
		if len(set.features) <= 1 {
			continue
		}

		return fmt.Errorf(
			"message %s combines multiple Go JSON encoding annotations (%s), which would generate duplicate MarshalJSON/UnmarshalJSON methods; split the annotations across wrapper messages or use only one direct JSON encoding feature per message",
			set.message.GoIdent.GoName,
			strings.Join(set.features, ", "),
		)
	}

	return nil
}
