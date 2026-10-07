package onkcompile

import "github.com/1homsi/onekit/internal/onkir"

func bindBodylessQueryFields(files []*onkir.File) {
	users := map[*onkir.Message][]*onkir.Method{}
	var order []*onkir.Message
	for _, file := range files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if _, seen := users[method.Request]; !seen {
					order = append(order, method.Request)
				}
				users[method.Request] = append(users[method.Request], method)
			}
		}
	}
	for _, message := range order {
		methods := users[message]
		if !allBodyless(methods) {
			continue
		}
		bindQueryFields(message, methods)
	}
}

func allBodyless(methods []*onkir.Method) bool {
	for _, method := range methods {
		verb, _ := method.Verb()
		if method.IsWebSocket() || isBodyBearingVerb(verb) {
			return false
		}
	}
	return true
}

func bindQueryFields(message *onkir.Message, methods []*onkir.Method) {
	inPath := map[string]bool{}
	for _, method := range methods {
		route, _ := method.Path()
		for _, name := range pathParameterNames(route) {
			inPath[name] = true
		}
	}
	for _, field := range message.Fields {
		if inPath[field.Name] || field.HasDecorator("query") {
			continue
		}
		if field.Oneof != nil || field.Repeated || field.Type == nil || field.Type.Kind != onkir.KindScalar || !isHTTPParameterScalar(field.Type.Scalar) {
			continue
		}
		field.Decorators = append(field.Decorators, onkir.Decorator{Name: "query"})
	}
}

func bodyRequestMessages(files []*onkir.File) map[*onkir.Message]bool {
	out := map[*onkir.Message]bool{}
	for _, file := range files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				verb, _ := method.Verb()
				if method.IsWebSocket() || isBodyBearingVerb(verb) {
					out[method.Request] = true
				}
			}
		}
	}
	return out
}

func requestPathNames(files []*onkir.File) map[*onkir.Message]map[string]bool {
	out := map[*onkir.Message]map[string]bool{}
	for _, file := range files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				route, _ := method.Path()
				for _, name := range pathParameterNames(route) {
					if out[method.Request] == nil {
						out[method.Request] = map[string]bool{}
					}
					out[method.Request][name] = true
				}
			}
		}
	}
	return out
}
