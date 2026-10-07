package onkcompile

import "github.com/1homsi/onekit/internal/onkir"

func markResponseOnlyMessages(files []*onkir.File, emitZero bool) {
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
		if message.ResponseOnly {
			for _, field := range message.Fields {
				if field.Nullable {
					field.AlwaysSent = true
				} else if emitZero {
					field.AlwaysSent = alwaysSentField(message, field)
				}
			}
		}
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

func alwaysSentField(owner *onkir.Message, field *onkir.Field) bool {
	if field.Optional || field.Repeated || field.Oneof != nil || field.Nullable || field.Type == nil {
		return false
	}
	for _, d := range field.Decorators {
		if d.Name != "required" {
			return false
		}
	}
	switch field.Type.Kind {
	case onkir.KindScalar:
		return field.Type.Scalar == onkir.ScalarTimestamp
	case onkir.KindMessage:
		target := field.Type.Message
		if target == nil || (len(target.Fields) == 1 && target.Fields[0].HasDecorator("unwrap")) {
			return false
		}
		return !reachesMessage(target, owner) && !reachesMessage(target, target)
	}
	return false
}

func reachesMessage(from, target *onkir.Message) bool {
	seen := map[*onkir.Message]bool{}
	var walk func(*onkir.Message) bool
	var walkType func(*onkir.Type) bool
	walkType = func(typ *onkir.Type) bool {
		if typ == nil {
			return false
		}
		switch typ.Kind {
		case onkir.KindMessage:
			return walk(typ.Message)
		case onkir.KindMap:
			return walkType(typ.MapValue)
		}
		return false
	}
	walk = func(m *onkir.Message) bool {
		if m == nil {
			return false
		}
		if m == target {
			return true
		}
		if seen[m] {
			return false
		}
		seen[m] = true
		for _, f := range m.Fields {
			if walkType(f.Type) {
				return true
			}
			if f.Oneof != nil {
				for _, v := range f.Oneof.Variants {
					if walkType(v.Type) {
						return true
					}
				}
			}
		}
		return false
	}
	for _, f := range from.Fields {
		if walkType(f.Type) {
			return true
		}
		if f.Oneof != nil {
			for _, v := range f.Oneof.Variants {
				if walkType(v.Type) {
					return true
				}
			}
		}
	}
	return false
}
