package onkcompile

import (
	"fmt"
	"math"
	"math/big"
	"regexp"

	"github.com/1homsi/onekit/internal/onklang"
)

var decimalLiteral = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

var integerBounds = map[string][2]*big.Int{
	"int32":  {big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)},
	"int64":  {big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)},
	"uint32": {big.NewInt(0), big.NewInt(math.MaxUint32)},
	"uint64": {big.NewInt(0), new(big.Int).SetUint64(math.MaxUint64)},
}

func validateNumericBounds(filePath string, line int, decorator onklang.Decorator, typ *onklang.TypeRef) error {
	switch decorator.Name {
	case "len", "min_items", "max_items":
		for _, arg := range decorator.Args {
			if !fitsInteger(arg.Value, big.NewInt(0), big.NewInt(math.MaxInt32)) {
				return &Error{Path: filePath, Line: line, Msg: fmt.Sprintf("@%s argument %q must be a non-negative integer", decorator.Name, arg.Value)}
			}
		}
	case "gt", "gte", "lt", "lte", "range":
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
