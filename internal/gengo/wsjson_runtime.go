package gengo

func writeWSJSONRuntime(p *Printer) {
	p.P(wsJSONRuntimeSource)
}

const wsJSONRuntimeSource = `var errWSJSON = errors.New("wsjson: fall back")

type wsJSON struct {
data []byte
pos int
err error
}

func (d *wsJSON) fail() { if d.err == nil { d.err = errWSJSON } }

func (d *wsJSON) ws() {
for d.pos < len(d.data) {
switch d.data[d.pos] {
case ' ', '\t', '\n', '\r':
d.pos++
default:
return
}
}
}

func (d *wsJSON) end() bool {
d.ws()
return d.err == nil && d.pos == len(d.data)
}

func (d *wsJSON) null() bool {
d.ws()
if d.pos+4 <= len(d.data) && string(d.data[d.pos:d.pos+4]) == "null" {
d.pos += 4
return true
}
return false
}

func (d *wsJSON) open(c byte) bool {
d.ws()
if d.err != nil || d.pos >= len(d.data) || d.data[d.pos] != c { d.fail(); return false }
d.pos++
return true
}

func (d *wsJSON) next(i int, c byte) bool {
if d.err != nil { return false }
d.ws()
if d.pos >= len(d.data) { d.fail(); return false }
if d.data[d.pos] == c { d.pos++; return false }
if i > 0 {
if d.data[d.pos] != ',' { d.fail(); return false }
d.pos++
d.ws()
}
return true
}

func (d *wsJSON) key() []byte {
d.ws()
s, escaped := d.stringBytes()
if escaped { d.fail() }
d.ws()
if d.err != nil || d.pos >= len(d.data) || d.data[d.pos] != ':' { d.fail(); return nil }
d.pos++
return s
}

func (d *wsJSON) stringBytes() ([]byte, bool) {
if d.err != nil || d.pos >= len(d.data) || d.data[d.pos] != '"' { d.fail(); return nil, false }
start := d.pos + 1
escaped := false
for i := start; i < len(d.data); i++ {
c := d.data[i]
switch {
case c == '"':
d.pos = i + 1
return d.data[start:i], escaped
case c == '\\':
escaped = true
if i+1 >= len(d.data) { d.fail(); return nil, false }
switch d.data[i+1] {
case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
i++
case 'u':
if i+5 >= len(d.data) { d.fail(); return nil, false }
for _, h := range d.data[i+2 : i+6] {
if !(h >= '0' && h <= '9' || h >= 'a' && h <= 'f' || h >= 'A' && h <= 'F') { d.fail(); return nil, false }
}
i += 5
default:
d.fail()
return nil, false
}
case c < 0x20:
d.fail()
return nil, false
case c >= 0x80:
escaped = true
}
}
d.fail()
return nil, false
}

func (d *wsJSON) str() string {
d.ws()
start := d.pos
s, escaped := d.stringBytes()
if d.err != nil { return "" }
if !escaped { return string(s) }
var out string
if err := json.Unmarshal(d.data[start:d.pos], &out); err != nil { d.fail() }
return out
}

func (d *wsJSON) number() []byte {
d.ws()
start := d.pos
i := d.pos
if i < len(d.data) && d.data[i] == '-' { i++ }
switch {
case i < len(d.data) && d.data[i] == '0':
i++
case i < len(d.data) && d.data[i] >= '1' && d.data[i] <= '9':
for i < len(d.data) && d.data[i] >= '0' && d.data[i] <= '9' { i++ }
default:
d.fail()
return nil
}
if i < len(d.data) && d.data[i] == '.' {
i++
digits := i
for i < len(d.data) && d.data[i] >= '0' && d.data[i] <= '9' { i++ }
if i == digits { d.fail(); return nil }
}
if i < len(d.data) && (d.data[i] == 'e' || d.data[i] == 'E') {
i++
if i < len(d.data) && (d.data[i] == '+' || d.data[i] == '-') { i++ }
digits := i
for i < len(d.data) && d.data[i] >= '0' && d.data[i] <= '9' { i++ }
if i == digits { d.fail(); return nil }
}
d.pos = i
return d.data[start:i]
}

func (d *wsJSON) boolean() bool {
d.ws()
switch {
case d.pos+4 <= len(d.data) && string(d.data[d.pos:d.pos+4]) == "true":
d.pos += 4
return true
case d.pos+5 <= len(d.data) && string(d.data[d.pos:d.pos+5]) == "false":
d.pos += 5
return false
}
d.fail()
return false
}

func (d *wsJSON) int(bits int) int64 {
n := d.number()
if d.err != nil { return 0 }
v, err := strconv.ParseInt(string(n), 10, bits)
if err != nil { d.fail() }
return v
}

func (d *wsJSON) uint(bits int) uint64 {
n := d.number()
if d.err != nil { return 0 }
v, err := strconv.ParseUint(string(n), 10, bits)
if err != nil { d.fail() }
return v
}

func (d *wsJSON) float(bits int) float64 {
n := d.number()
if d.err != nil { return 0 }
v, err := strconv.ParseFloat(string(n), bits)
if err != nil { d.fail() }
return v
}

func (d *wsJSON) intString(bits int) int64 {
s := d.str()
if d.err != nil || s == "" { return 0 }
v, err := strconv.ParseInt(s, 10, bits)
if err != nil { d.fail() }
return v
}

func (d *wsJSON) uintString(bits int) uint64 {
s := d.str()
if d.err != nil || s == "" { return 0 }
v, err := strconv.ParseUint(s, 10, bits)
if err != nil { d.fail() }
return v
}

func (d *wsJSON) bytes() []byte {
d.ws()
s, escaped := d.stringBytes()
if d.err != nil { return nil }
if escaped { d.fail(); return nil }
out := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
n, err := base64.StdEncoding.Decode(out, s)
if err != nil { d.fail(); return nil }
return out[:n]
}

func (d *wsJSON) raw() []byte {
d.ws()
start := d.pos
d.skip(0)
if d.err != nil { return nil }
return d.data[start:d.pos]
}

func (d *wsJSON) skip(depth int) {
d.ws()
if d.err != nil || d.pos >= len(d.data) || depth > 1000 { d.fail(); return }
switch c := d.data[d.pos]; {
case c == '{':
d.pos++
for i := 0; d.next(i, '}'); i++ {
d.ws()
d.stringBytes()
d.ws()
if d.err != nil || d.pos >= len(d.data) || d.data[d.pos] != ':' { d.fail(); return }
d.pos++
d.skip(depth + 1)
}
case c == '[':
d.pos++
for i := 0; d.next(i, ']'); i++ { d.skip(depth + 1) }
case c == '"':
d.stringBytes()
case c == 't' || c == 'f':
d.boolean()
case c == 'n':
if !d.null() { d.fail() }
default:
d.number()
}
}

func (d *wsJSON) delegate(v any) {
raw := d.raw()
if d.err != nil { return }
if err := json.Unmarshal(raw, v); err != nil { d.fail() }
}

func wsFoldKey(key []byte, keys ...string) bool {
for _, k := range keys {
if strings.EqualFold(string(key), k) { return true }
}
return false
}

func wsAppendKey(b []byte, first *bool, key string) []byte {
if !*first { b = append(b, ',') }
*first = false
return append(b, key...)
}

func wsAppendString(b []byte, s string) []byte {
const hex = "0123456789abcdef"
b = append(b, '"')
start := 0
for i := 0; i < len(s); {
c := s[i]
if c < utf8.RuneSelf {
if c >= 0x20 && c != '"' && c != '\\' { i++; continue }
b = append(b, s[start:i]...)
switch c {
case '"', '\\':
b = append(b, '\\', c)
case '\n':
b = append(b, '\\', 'n')
case '\r':
b = append(b, '\\', 'r')
case '\t':
b = append(b, '\\', 't')
default:
b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
}
i++
start = i
continue
}
r, size := utf8.DecodeRuneInString(s[i:])
if r == utf8.RuneError && size == 1 {
b = append(b, s[start:i]...)
b = append(b, "\\ufffd"...)
i += size
start = i
continue
}
if r == '\u2028' || r == '\u2029' {
b = append(b, s[start:i]...)
b = append(b, '\\', 'u', '2', '0', '2', hex[r&0xF])
i += size
start = i
continue
}
i += size
}
b = append(b, s[start:]...)
return append(b, '"')
}

func wsAppendFloat(b []byte, f float64, bits int) ([]byte, error) {
if math.IsInf(f, 0) || math.IsNaN(f) { return b, errWSJSON }
format := byte('f')
if abs := math.Abs(f); abs != 0 {
if bits == 64 && (abs < 1e-6 || abs >= 1e21) || bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) { format = 'e' }
}
b = strconv.AppendFloat(b, f, format, -1, bits)
if format == 'e' {
n := len(b)
if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
b[n-2] = b[n-1]
b = b[:n-1]
}
}
return b, nil
}

func wsAppendBytes(b []byte, v []byte) []byte {
b = append(b, '"')
b = base64.StdEncoding.AppendEncode(b, v)
return append(b, '"')
}

func wsAppendStd(b []byte, v any) ([]byte, error) {
raw, err := json.Marshal(v)
if err != nil { return b, err }
return append(b, raw...), nil
}
`
