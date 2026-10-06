package onkcompile

import "github.com/1homsi/onekit/internal/onkir"

func markResponseOnlyMessages(files []*onkir.File) {
	requests := map[*onkir.Message]bool{}
	responses := map[*onkir.Message]bool{}
	for _, file := range files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				reachMessages(method.Request, requests)
				reachMessages(method.Response, responses)
				for _, errorType := range method.ErrorTypes {
					reachMessages(errorType, responses)
				}
			}
		}
	}
	var mark func(*onkir.Message)
	mark = func(message *onkir.Message) {
		message.ResponseOnly = responses[message] && !requests[message]
		for _, nested := range message.Nested {
			mark(nested)
		}
	}
	for _, file := range files {
		for _, message := range file.Messages {
			mark(message)
		}
	}
}

func reachMessages(message *onkir.Message, seen map[*onkir.Message]bool) {
	if message == nil || seen[message] {
		return
	}
	seen[message] = true
	for _, field := range message.Fields {
		reachType(field.Type, seen)
		if field.Oneof != nil {
			for _, variant := range field.Oneof.Variants {
				reachType(variant.Type, seen)
			}
		}
	}
}

func reachType(typ *onkir.Type, seen map[*onkir.Message]bool) {
	if typ == nil {
		return
	}
	switch typ.Kind {
	case onkir.KindMessage:
		reachMessages(typ.Message, seen)
	case onkir.KindMap:
		reachType(typ.MapValue, seen)
	}
}
