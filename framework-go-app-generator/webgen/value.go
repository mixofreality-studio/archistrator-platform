package webgen

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Value is a JSON value exactly as JavaScript holds it after js-yaml or
// JSON.parse: nil, bool, float64 (a JS Number), string, []Value or *Object.
//
// The generators this package replaces are JavaScript, and their output is the
// byte-identical reference (archistrator's committed webApp). So the value model
// keeps JavaScript's semantics where they show in the bytes: object keys keep
// insertion order except that array-index keys come first, ascending (Object
// keys order, ECMA-262 OrdinaryOwnPropertyKeys), and numbers print the way
// Number.prototype.toString prints them.
type Value = any

// Object is a JavaScript plain object: string keys, JS enumeration order.
type Object struct {
	keys []string
	vals map[string]Value
}

// NewObject returns an empty object.
func NewObject() *Object {
	return &Object{vals: map[string]Value{}}
}

// Set assigns key. A new key is appended; an existing key keeps its position,
// as a JavaScript assignment does.
func (o *Object) Set(key string, v Value) *Object {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
	return o
}

// Get returns the key's value and whether the object has it.
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Len is the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Keys returns the keys in JavaScript enumeration order: array-index keys
// ascending first, then every other key in insertion order.
func (o *Object) Keys() []string {
	var index, other []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			index = append(index, k)
		} else {
			other = append(other, k)
		}
	}
	sort.SliceStable(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 32)
		b, _ := strconv.ParseUint(index[j], 10, 32)
		return a < b
	})
	return append(index, other...)
}

// isArrayIndex reports whether k is a canonical array index: the decimal form
// of an integer in [0, 2^32-2], with no sign and no leading zero.
func isArrayIndex(k string) bool {
	if k == "" || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for _, c := range k {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < math.MaxUint32
}

// obj reads v as an object, or nil.
func obj(v Value) *Object {
	o, _ := v.(*Object)
	return o
}

// path walks nested objects by key; nil when any step is missing or not an object.
func path(v Value, keys ...string) Value {
	for _, k := range keys {
		o := obj(v)
		if o == nil {
			return nil
		}
		v, _ = o.Get(k)
	}
	return v
}

// Stringify renders v exactly as JSON.stringify(v, null, 2) does.
func Stringify(v Value) string {
	var b strings.Builder
	writeValue(&b, v, "")
	return b.String()
}

func writeValue(b *strings.Builder, v Value, indent string) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		writeNumber(b, x)
	case string:
		b.WriteString(quoteJS(x))
	case []Value:
		writeArray(b, x, indent)
	case *Object:
		writeObject(b, x, indent)
	default:
		panic("webgen: Stringify of a non-JSON value")
	}
}

func writeNumber(b *strings.Builder, f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		b.WriteString("null")
		return
	}
	b.WriteString(jsNumber(f))
}

func writeArray(b *strings.Builder, a []Value, indent string) {
	if len(a) == 0 {
		b.WriteString("[]")
		return
	}
	inner := indent + "  "
	b.WriteString("[\n")
	for i, v := range a {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(inner)
		writeValue(b, v, inner)
	}
	b.WriteString("\n" + indent + "]")
}

func writeObject(b *strings.Builder, o *Object, indent string) {
	if o.Len() == 0 {
		b.WriteString("{}")
		return
	}
	inner := indent + "  "
	b.WriteString("{\n")
	for i, k := range o.Keys() {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(inner + quoteJS(k) + ": ")
		v, _ := o.Get(k)
		writeValue(b, v, inner)
	}
	b.WriteString("\n" + indent + "}")
}

// quoteJS is JSON.stringify's string quoting (ES2019 well-formed): only the
// quote, the backslash and the C0 controls are escaped, and a control with no
// short form becomes \u00xx in lower-case hex. Unlike encoding/json, '<', '>',
// '&', U+2028 and U+2029 are written as themselves.
func quoteJS(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		b.WriteString(escapeRune(r))
	}
	b.WriteByte('"')
	return b.String()
}

var shortEscapes = map[rune]string{
	'"': `\"`, '\\': `\\`, '\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`,
}

func escapeRune(r rune) string {
	if e, ok := shortEscapes[r]; ok {
		return e
	}
	if r < 0x20 {
		return `\u00` + strconv.FormatInt(int64(r)>>4, 16) + strconv.FormatInt(int64(r)&0xf, 16)
	}
	return string(r)
}

// jsNumber is Number.prototype.toString for a finite f (ECMA-262
// Number::toString, radix 10): the shortest round-tripping digits, written in
// positional notation for a decimal exponent in (-7, 21] and in exponent
// notation otherwise.
func jsNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	digits, n := shortestDigits(f)
	return sign + placeDigits(digits, n)
}

// shortestDigits returns the shortest decimal digits d1..dk of f and n, the
// position of the decimal point: f = 0.d1..dk × 10^n.
func shortestDigits(f float64) (string, int) {
	e := strconv.FormatFloat(f, 'e', -1, 64) // "d.ddde±XX"
	mant, exp, _ := strings.Cut(e, "e")
	x, _ := strconv.Atoi(exp)
	return strings.Replace(mant, ".", "", 1), x + 1
}

func placeDigits(digits string, n int) string {
	k := len(digits)
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	exp := n - 1
	expSign := "+"
	if exp < 0 {
		expSign, exp = "-", -exp
	}
	mant := digits[:1]
	if k > 1 {
		mant += "." + digits[1:]
	}
	return mant + "e" + expSign + strconv.Itoa(exp)
}
