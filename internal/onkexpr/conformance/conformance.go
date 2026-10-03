package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/1homsi/onekit/internal/onkcompile"
	"github.com/1homsi/onekit/internal/onkexpr"
	"github.com/1homsi/onekit/internal/onkir"
	"github.com/1homsi/onekit/internal/onklang"
)

const header = `package conformance

enum Status { OPEN DONE ARCHIVED }

message Meta {
  owner: string
  tags: string[]
}

`

const subjectFields = `  name: string @rule("size(value) <= 6", "field-name-size")
  count: int32 @rule("value >= -100", "field-count-floor")
  big: int64
  zero: int32
  ratio: float64
  flag: bool
  maybe: string?
  maybe_n: int32?
  status: Status
  tags: string[]
  nums: int32[]
  labels: map[string, string]
  scores: map[string, int32]
  meta: Meta
  metas: Meta[]
  data: bytes
`

var Rules = []string{
	"self.count + 1 > 10",
	"self.count * 2 == 14",
	"self.count / 2 == 3",
	"self.count / 2 == -3",
	"self.count % 3 == 1",
	"self.count % 3 == -1",
	"self.big + 1 > self.big",
	"self.big - 1 < self.big",
	"self.big * 2 > 0",
	"-self.big < 0",
	"self.big / -1 != 0",
	"self.big % -1 == 0",
	"1 / self.zero == 0",
	"1 % self.zero == 0",
	"self.count < self.big",
	"9223372036854775807 + 0 == 9223372036854775807",
	"-9223372036854775808 + 0 < 0",
	"(self.count - 3) * (self.count - 3) >= 0",
	"self.ratio * 2.0 > 1.0",
	"self.ratio / 0.0 > 1.0",
	"double(self.count) / 2.0 == 3.5",
	"int(self.ratio) == 3",
	"int(self.ratio) == -3",
	"int(1e30) == 0",
	"0.1 + 0.2 > 0.3",
	"0.1 + 0.2 == 0.30000000000000004",
	"self.ratio > 0.0 ? self.ratio < 100.0 : self.ratio > -100.0",
	"double(self.big) > 0.0",
	"1e300 * 1e300 > 0.0",
	"size(self.name) == 3",
	"size(self.name) == 1",
	"self.name == 'abc'",
	"self.name == 'e\\u0301'",
	"self.name == '\\u00e9'",
	"self.name != ''",
	"self.name.startsWith('ab')",
	"self.name.endsWith('bc')",
	"self.name.contains('b')",
	"self.name.contains('')",
	"self.name.matches('[a-z]+')",
	"self.name.matches('[a-z]{2,4}')",
	"self.name.matches('(ab)+c?')",
	"self.name.matches('[^a]+')",
	"self.name.matches('[a-z]*')",
	"self.name.matches('a|ab|abc')",
	"matches(self.name, '[0-9]+(\\\\.[0-9]+)?')",
	"self.name.matches('[\\u00e0-\\u00ff]+')",
	"self.flag ? self.count > 0 : self.count < 0",
	"!self.flag",
	"self.flag && self.count > 0",
	"self.flag || self.count > 0",
	"false && 1 / self.zero == 0",
	"true || 1 / self.zero == 0",
	"1 / self.zero == 0 || true",
	"true && 1 / self.zero == 0",
	"has(self.maybe)",
	"!has(self.maybe) || self.maybe != ''",
	"self.maybe == ''",
	"has(self.maybe_n)",
	"self.maybe_n == 0",
	"has(self.name)",
	"has(self.count)",
	"has(self.flag)",
	"has(self.tags)",
	"has(self.labels)",
	"has(self.meta)",
	"has(self.meta.owner)",
	"self.meta.owner == ''",
	"self.status == 'OPEN'",
	"self.status != 'DONE'",
	"self.status in ['OPEN', 'DONE']",
	"'ARCHIVED' == self.status",
	"size(self.tags) <= 1",
	"self.tags.size() == 0",
	"self.tags.all(t, size(t) > 0)",
	"self.tags.exists(t, t == 'x')",
	"'x' in self.tags",
	"self.nums[0] == 5",
	"self.nums[1] == 7",
	"self.nums[-1] == 5",
	"self.nums.all(n, n > 0)",
	"self.nums.exists(n, n % 2 == 0)",
	"self.nums.all(n, self.nums.exists(m, m >= n))",
	"self.metas.all(m, m.owner != '')",
	"self.metas.exists(m, size(m.tags) > 0)",
	"5 in self.nums",
	"self.count in [1, 2, 7]",
	"self.name in ['abc', 'x']",
	"'k' in self.labels",
	"self.labels['k'] == 'v'",
	"size(self.labels) < 3",
	"self.scores['a'] > 1",
	"'a' in self.scores && self.scores['a'] > 1",
	"self.meta.owner != ''",
	"size(self.meta.tags) == 1",
	"self.meta.tags.all(t, t != '')",
	"size(self.data) == 3",
	"size(self.data) == 0",
	"self.count < self.big",
	"self.big >= -9223372036854775807",
}

var Inputs = []string{
	`{}`,
	`{"name":"abc","count":7,"big":"5","ratio":3.5,"flag":true,"status":"OPEN","tags":["x","y"],"nums":[5,7],"labels":{"k":"v"},"scores":{"a":2},"meta":{"owner":"o","tags":["t"]},"metas":[{"owner":"a","tags":["1"]}],"data":"AQID"}`,
	`{"name":"abcdef","count":-7,"big":"-5","ratio":-3.99,"flag":false,"maybe":"","maybe_n":0,"status":"DONE","tags":["","x"],"nums":[2,4,6],"labels":{"a":"b","c":"d","e":"f"},"scores":{"a":1},"meta":{"owner":""},"metas":[{"owner":""}]}`,
	`{"name":"\u00e9","count":0,"big":"9223372036854775807","ratio":0.0,"status":"ARCHIVED","nums":[5],"meta":{}}`,
	`{"name":"\u00e9","count":1,"big":"-9223372036854775808","ratio":1e300,"flag":true,"maybe":"x","maybe_n":3}`,
	`{"name":"\ud83d\udc4d","count":100,"big":"1","ratio":99.9,"tags":["x"],"nums":[-1,0]}`,
	`{"name":"h\u00e9\u00e9","count":-100,"big":"9223372036854775806","ratio":-100.0,"status":"DONE","labels":{"k":"v"},"scores":{"a":0,"b":5}}`,
	`{"name":"ab\n","count":10,"big":"0","ratio":0.1,"flag":true,"data":""}`,
	`{"name":"12.5","count":3,"big":"-1","ratio":3.0,"maybe":"y","nums":[1,2,3,4],"metas":[{"owner":"z","tags":["a","b"]},{"owner":"w"}]}`,
	`{"name":"ab","count":-101,"big":"2","ratio":2.5,"flag":false,"tags":[],"nums":[],"labels":{},"scores":{}}`,
	`{"name":"abc","count":2147483647,"big":"4611686018427387904","ratio":-0.5,"status":"OPEN","meta":{"owner":"abc","tags":["a","b"]}}`,
	`{"name":"\u00e0\u00e0","count":14,"big":"-4611686018427387905","ratio":1.5,"nums":[7,5,5],"tags":["x","x"]}`,
	`{"name":"e\u0301","count":-2147483648,"big":"3037000500","ratio":123456.789,"flag":true,"maybe_n":-1}`,
}

func SchemaSource() string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("message Subject")
	for i, rule := range Rules {
		b.WriteString("\n  @rule(")
		b.WriteString(strconv.Quote(rule))
		b.WriteString(", ")
		b.WriteString(strconv.Quote(Label(i)))
		b.WriteString(")")
	}
	b.WriteString(" {\n")
	b.WriteString(subjectFields)
	b.WriteString("}\n")
	return b.String()
}

func Label(index int) string {
	return fmt.Sprintf("r%03d", index)
}

func Compile() (*onkir.Package, *onkir.File, *onkir.Message, error) {
	ast, err := onklang.Parse(SchemaSource())
	if err != nil {
		return nil, nil, nil, err
	}
	pkg, err := onkcompile.Compile([]onkcompile.Source{{Path: "conformance.onk", AST: ast}})
	if err != nil {
		return nil, nil, nil, err
	}
	for _, f := range pkg.Files {
		for _, m := range f.Messages {
			if m.Name == "Subject" {
				return pkg, f, m, nil
			}
		}
	}
	return nil, nil, nil, errors.New("the Subject message is missing")
}

func Expected() ([]string, error) {
	_, _, subject, err := Compile()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(Inputs))
	for i, input := range Inputs {
		var raw any
		if err := json.Unmarshal([]byte(input), &raw); err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		decoded, err := onkexpr.FromJSON(onkexpr.MessageType(subject), raw)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		message, ok := decoded.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input %d is not an object", i)
		}
		failed, err := onkexpr.Violations(subject, message)
		if err != nil {
			return nil, err
		}
		sort.Strings(failed)
		out[i] = strings.Join(failed, ",")
	}
	return out, nil
}

var Patterns = []string{
	`abc`, `[a-z]+`, `[A-Za-z0-9_]*`, `[^a]+`, `a|b|c`, `(ab)+`, `(?:ab)*c?`, `[0-9]{3}`, `[0-9]{2,4}`,
	`[0-9]+(\.[0-9]+)?`, `[a-z]+(-[a-z]+)?`, `(ab|cd)?x`, `[à-ÿ]+`, `a{0,3}`, `\\`, `\n`, `[\]]`, `[a\-z]`, `a-b`,
	`[a&b]`, `[a|b]`, `[~]`, `a\/b`, `[\/]`, `\+\*\?`, `x{1000}`, `(a?)b`, `[a-z]{1,63}`, `[^\n]+`, `a|ab|abc`,
	`[\^]`, `\.`, `[.]`, `(ab|cd)(ef|gh)`, `[0-9a-f]{8}-[0-9a-f]{4}`, `\(a\)`, `[(){}]+`, `[^a-c]`, `(?:a|b)c`,
}

var PatternProbes = []string{
	"", "a", "b", "c", "ab", "abc", "abcd", "aba", "ababc", "abab", "x", "ax", "abx", "cdx", "é", "ée", "\u00e9\u00ff", "\U0001F44D",
	"e\u0301", "12", "123", "1234", "12345", "12.5", "12.", ".5", "1.2.3", "a-b", "a--b", "ab-cd", "-", "a\n", "\n", "a\nb", "x\n",
	"abcabc", "a1", "A", "_", "a/b", "/", "ab\\", "\\", "+*?", "(a)", "((", "{}", "ef", "cdef", "abgh", "abefgh", "deadbeef-0123", "a&b", "a|b", "~", "^", ".",
	"ac", "bc", "cc", "d", "b1", "xxxx", " ", "aaaa", "aaa",
}
