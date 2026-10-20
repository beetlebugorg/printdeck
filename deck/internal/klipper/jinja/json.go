package jinja

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"printdeck/internal/klipper/pyfmt"
)

// MarshalJSON writes the dict as a JSON object, with the keys in the order of the dict.
func (d *Dict) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range d.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(d.m[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// JSONDumps is Python's json.dumps() of a value, with the default options: ", " and ": " as the
// separators, and the characters that are not ASCII as \u escapes.
func JSONDumps(v any) string {
	var b strings.Builder
	jsonDump(&b, v)
	return b.String()
}

func jsonDump(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		switch {
		case math.IsNaN(x):
			b.WriteString("NaN")
		case math.IsInf(x, 1):
			b.WriteString("Infinity")
		case math.IsInf(x, -1):
			b.WriteString("-Infinity")
		default:
			b.WriteString(pyfmt.Float(x))
		}
	case string:
		jsonString(b, x)
	case []any:
		jsonList(b, x)
	case Tuple:
		jsonList(b, x)
	case *Dict:
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteString(", ")
			}
			jsonString(b, k)
			b.WriteString(": ")
			jsonDump(b, x.m[k])
		}
		b.WriteByte('}')
	case map[string]any:
		jsonDump(b, DictOf(x))
	default:
		panic(&Error{"TypeError", fmt.Sprintf("Object of type %s is not JSON serializable", typeName(v))})
	}
}

func jsonList(b *strings.Builder, l []any) {
	b.WriteByte('[')
	for i, it := range l {
		if i > 0 {
			b.WriteString(", ")
		}
		jsonDump(b, it)
	}
	b.WriteByte(']')
}

func jsonString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || (r > 0x7e && r < 0x10000):
				fmt.Fprintf(b, `\u%04x`, r)
			case r >= 0x10000:
				r1, r2 := utf16.EncodeRune(r)
				fmt.Fprintf(b, `\u%04x\u%04x`, r1, r2)
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// JSONLoads is Python's json.loads(): objects are dicts in the order of the text, integers are
// int64, and the other numbers are float64. An error is a Python ValueError.
func JSONLoads(s string) (v any, err error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(error)
			if !ok {
				panic(r)
			}
			v, err = nil, &Error{"JSONDecodeError", e.Error()}
		}
	}()
	v = jsonValue(dec, jsonToken(dec))
	if _, terr := dec.Token(); terr == nil {
		return nil, &Error{"JSONDecodeError", "Extra data"}
	}
	return v, nil
}

func jsonToken(dec *json.Decoder) json.Token {
	t, err := dec.Token()
	if err != nil {
		panic(err)
	}
	return t
}

func jsonValue(dec *json.Decoder, t json.Token) any {
	switch x := t.(type) {
	case json.Delim:
		switch x {
		case '[':
			out := []any{}
			for dec.More() {
				out = append(out, jsonValue(dec, jsonToken(dec)))
			}
			jsonToken(dec)
			return out
		case '{':
			d := NewDict()
			for dec.More() {
				k := jsonToken(dec).(string)
				d.Set(k, jsonValue(dec, jsonToken(dec)))
			}
			jsonToken(dec)
			return d
		}
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			if n, err := strconv.ParseInt(string(x), 10, 64); err == nil {
				return n
			}
		}
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil {
			panic(err)
		}
		return f
	case string, bool, nil:
		return x
	}
	panic(fmt.Errorf("unexpected JSON token %v", t))
}

