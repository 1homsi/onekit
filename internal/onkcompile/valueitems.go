package onkcompile

import "github.com/1homsi/onekit/internal/onkir"

func markValueItems(files []*onkir.File) {
	websocket := map[*onkir.Message]bool{}
	for _, file := range files {
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if method.IsWebSocket() {
					reachMessages(method.Request, websocket)
					reachMessages(method.Response, websocket)
				}
			}
		}
	}
	var mark func(*onkir.Message)
	mark = func(message *onkir.Message) {
		if !websocket[message] {
			for _, field := range message.Fields {
				if field.Repeated && field.Oneof == nil && field.Type != nil && field.Type.Kind == onkir.KindMessage {
					field.ValueItems = true
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
