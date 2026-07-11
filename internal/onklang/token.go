package onklang

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
	Kind Kind
	Text string
	Line int
	Col  int
	Doc  string
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
