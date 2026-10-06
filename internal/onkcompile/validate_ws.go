package onkcompile

import (
	"fmt"

	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

// validateWSCorrelation enforces that @ws_id, if used at all on a @ws
// method's request/response (directly or within a oneof variant's own
// message), appears at most once per scope and shares one scalar type
// across every scope it appears in - generators emit a single generic
// pending-call map per method and need one consistent key type to key it on.
func validateWSCorrelation(filePath string, method *onkir.Method) error {
	requestFields, err := collectWSIDFields(filePath, method.Name, "request", method.Request)
	if err != nil {
		return err
	}
	responseFields, err := collectWSIDFields(filePath, method.Name, "response", method.Response)
	if err != nil {
		return err
	}
	for _, frame := range []struct {
		direction string
		message   *onkir.Message
	}{{"request", method.Request}, {"response", method.Response}} {
		if err := validateWSCancelVariants(filePath, method.Name, frame.direction, frame.message); err != nil {
			return err
		}
	}
	for _, message := range []*onkir.Message{method.Request, method.Response} {
		if err := validateWSTimeoutFields(filePath, method.Name, message); err != nil {
			return err
		}
	}
	all := append(append([]*onkir.Field{}, requestFields...), responseFields...)
	if len(all) == 0 {
		return nil
	}
	kind := all[0].Type.Scalar
	for _, field := range all[1:] {
		if field.Type == nil || field.Type.Kind != onkir.KindScalar || field.Type.Scalar != kind {
			return &Error{Path: filePath, Msg: fmt.Sprintf(
				"@ws_id fields on RPC %s must share one scalar type; found both %s and %s",
				method.Name, kind, field.Type.Scalar,
			)}
		}
	}
	return nil
}

// collectWSIDFields walks a @ws method's request or response message,
// gathering every @ws_id field found directly on it or on any oneof
// variant's own message, and rejects more than one per scope along the way.
func collectWSIDFields(filePath, methodName, direction string, message *onkir.Message) ([]*onkir.Field, error) {
	var found []*onkir.Field
	field, err := wsIDFieldInScope(filePath, methodName, direction+" message", message.Fields)
	if err != nil {
		return nil, err
	}
	if field != nil {
		found = append(found, field)
	}
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, variant := range f.Oneof.Variants {
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil {
				continue
			}
			variantField, err := wsIDFieldInScope(filePath, methodName, direction+" oneof variant "+variant.Name, variant.Type.Message.Fields)
			if err != nil {
				return nil, err
			}
			if variantField != nil {
				found = append(found, variantField)
			}
		}
	}
	return found, nil
}

// validateWSCancelVariants allows at most one @ws_cancel variant per frame
// message and requires its message to carry @ws_id directly: generated code
// builds the cancel frame from nothing but the abandoned call's id.
func validateWSCancelVariants(filePath, methodName, direction string, message *onkir.Message) error {
	var found *onkir.OneofVariant
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, variant := range f.Oneof.Variants {
			if !variant.IsWSCancel() {
				continue
			}
			if found != nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf(
					"%s message on RPC %s has more than one @ws_cancel variant (%s and %s)",
					direction, methodName, found.Name, variant.Name,
				)}
			}
			found = variant
			if variant.Type == nil || variant.Type.Kind != onkir.KindMessage || variant.Type.Message == nil || onkir.FindWSIDDirect(variant.Type.Message) == nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf(
					"@ws_cancel variant %s on RPC %s must be a message with a @ws_id field",
					variant.Name, methodName,
				)}
			}
		}
	}
	return nil
}

// wsIDFieldInScope returns the single @ws_id field among fields, or an error
// if more than one carries the decorator.
func wsIDFieldInScope(filePath, methodName, scope string, fields []*onkir.Field) (*onkir.Field, error) {
	var found *onkir.Field
	for _, field := range fields {
		if !field.HasDecorator(wsIDDecorator) {
			continue
		}
		if found != nil {
			return nil, &Error{Path: filePath, Msg: fmt.Sprintf("%s on RPC %s has more than one @ws_id field", scope, methodName)}
		}
		found = field
	}
	return found, nil
}

func validateWSTimeoutFields(filePath, methodName string, message *onkir.Message) error {
	check := func(scope string, m *onkir.Message) error {
		count := 0
		for _, f := range m.Fields {
			if !f.HasDecorator("ws_timeout") {
				continue
			}
			count++
			if count > 1 {
				return &Error{Path: filePath, Msg: fmt.Sprintf("%s on RPC %s has more than one @ws_timeout field", scope, methodName)}
			}
			if onkir.FindWSIDDirect(m) == nil {
				return &Error{Path: filePath, Msg: fmt.Sprintf("@ws_timeout field %s on RPC %s must sit next to a @ws_id field", f.Name, methodName)}
			}
		}
		return nil
	}
	if err := check("message "+message.Name, message); err != nil {
		return err
	}
	for _, f := range message.Fields {
		if f.Oneof == nil {
			continue
		}
		for _, v := range f.Oneof.Variants {
			if v.Type != nil && v.Type.Kind == onkir.KindMessage && v.Type.Message != nil {
				if err := check("oneof variant "+v.Name, v.Type.Message); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateWSFieldDecorator(filePath string, field *onklang.FieldDecl, name string) error {
	if name == "ws_timeout" && (field.Repeated || field.Optional || !isIntegerTypeRef(field.Type)) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@ws_timeout requires a non-repeated, non-optional integer field"}
	}
	if name == "raw" && (field.Repeated || field.Optional || !(isScalarNamed(field.Type, "string") || isScalarNamed(field.Type, "bytes"))) {
		return &Error{Path: filePath, Line: field.Line, Msg: "@raw requires a non-repeated, non-optional string or bytes field"}
	}
	return nil
}
