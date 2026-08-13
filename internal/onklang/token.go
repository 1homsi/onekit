package onklang

import "fmt"

// Error is a location-aware lexer/parser diagnostic. Keeping the location as
// structured data lets the CLI expose editor-friendly JSON without parsing
// the human-readable error string.
type Error struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("onklang:%d:%d: %s", e.Line, e.Column, e.Message)
}

type Kind int

const (
	EOF Kind = iota
	ILLEGAL

	IDENT
	STRING
	INT
	FLOAT

	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	LPAREN
	RPAREN
	COMMA
	DOT
	ARROW
	COLON
	AT
	QUESTION
	PIPE
)

type Token struct {
	Kind            Kind
	Text            string
	Line            int
	Col             int
	Doc             string
	LeadingComments []string
}

func (k Kind) String() string {
	switch k {
	case EOF:
		return "EOF"
	case ILLEGAL:
		return "ILLEGAL"
	case IDENT:
		return "IDENT"
	case STRING:
		return "STRING"
	case INT:
		return "INT"
	case FLOAT:
		return "FLOAT"
	case LBRACE:
		return "{"
	case RBRACE:
		return "}"
	case LBRACKET:
		return "["
	case RBRACKET:
		return "]"
	case LPAREN:
		return "("
	case RPAREN:
		return ")"
	case COMMA:
		return ","
	case DOT:
		return "."
	case ARROW:
		return "->"
	case COLON:
		return ":"
	case AT:
		return "@"
	case QUESTION:
		return "?"
	case PIPE:
		return "|"
	default:
		return "UNKNOWN"
	}
}
