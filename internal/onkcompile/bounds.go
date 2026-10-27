package onkcompile

import (
	"fmt"
	"math"
	"math/big"
	"regexp"

	"github.com/1homsi/onekit/internal/onkschema"
)

var decimalLiteral = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

var integerBounds = map[string][2]*big.Int{
	"int32":  {big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)},
	"int64":  {big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)},
	"uint32": {big.NewInt(0), big.NewInt(math.MaxUint32)},
	"uint64": {big.NewInt(0), new(big.Int).SetUint64(math.MaxUint64)},
}

var countDecorators = map[string]bool{"len": true, "min_items": true, "max_items": true}

var comparisonDecorators = map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true, "range": true}

func validateNumericBounds(filePath string, line int, decorator onkschema.Decorator, typ *onkschema.TypeRef) error {
	if err := validateArgumentKinds(filePath, line, decorator, typ); err != nil {
		return err
	}
	switch {
	case countDecorators[decorator.Name]:
		for _, arg := range decorator.Args {
			if !fitsInteger(arg.Value, big.NewInt(0), big.NewInt(math.MaxInt32)) {
				return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be a non-negative integer", decorator.Name, arg.Value)}
			}
		}
	case decorator.Name == "in" && typ != nil:
		if bounds, ok := integerBounds[typ.Name]; ok {
			for _, arg := range decorator.Args {
				if !fitsInteger(arg.Value, bounds[0], bounds[1]) {
					return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@in value %q must be an integer within the range of %s", arg.Value, typ.Name)}
				}
			}
		}
	case comparisonDecorators[decorator.Name]:
		for _, arg := range decorator.Args {
			if !decimalLiteral.MatchString(arg.Value) {
				return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be a finite decimal number", decorator.Name, arg.Value)}
			}
			if typ == nil {
				continue
			}
			if bounds, ok := integerBounds[typ.Name]; ok && !fitsInteger(arg.Value, bounds[0], bounds[1]) {
				return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be an integer within the range of %s", decorator.Name, arg.Value, typ.Name)}
			}
		}
	}
	return nil
}

func fitsInteger(value string, minimum, maximum *big.Int) bool {
	n, ok := new(big.Int).SetString(value, 10)
	return ok && n.Cmp(minimum) >= 0 && n.Cmp(maximum) <= 0
}

// validateArgumentKinds rejects a number written as a string and a string
// written as a number: @range("1", "5") and @in(1, true) on a string field
// compiled to code that compares the wrong kind of value.
func validateArgumentKinds(filePath string, line int, decorator onkschema.Decorator, typ *onkschema.TypeRef) error {
	wantQuoted, wantNumber := false, false
	switch {
	case countDecorators[decorator.Name], comparisonDecorators[decorator.Name]:
		wantNumber = true
	case decorator.Name == "in" && typ != nil && typ.Name == "string":
		wantQuoted = true
	case decorator.Name == "in" && typ != nil:
		_, wantNumber = integerBounds[typ.Name]
	}
	for _, arg := range decorator.Args {
		switch {
		case wantNumber && arg.Quoted && decimalLiteral.MatchString(arg.Value):
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be a number, not a string", decorator.Name, arg.Value)}
		case wantQuoted && !arg.Quoted:
			return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s value %s must be a quoted string", decorator.Name, arg.Value)}
		}
	}
	return nil
}
