// Package jinja is a template engine for the Klipper templates (gcode_macro, the display, the
// menus): the part of Jinja2 that these templates use, with the values of Python.
//
// It follows Jinja2 3.1 (https://jinja.palletsprojects.com) with the settings of Klipper:
// "{%" and "%}" for blocks, "{" and "}" for expressions, and the default undefined value. A
// template gives the same text as Jinja2 for the same values. The tests compare the output with
// Jinja2 (tools/klipper-reference/dumpjinja.py).
//
// The values are the values of Python: nil (None), bool, int64 (int), float64 (float), string,
// []any (list), Tuple, *Dict, Func, and Object for the values of the printer.
package jinja

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"printdeck/internal/klipper/pyfmt"
)

// Tuple is a Python tuple.
type Tuple []any

// Dict is a Python dict with string keys, in insertion order.
type Dict struct {
	keys []string
	m    map[string]any
}

// NewDict makes an empty dict.
func NewDict() *Dict { return &Dict{m: map[string]any{}} }

// DictOf makes a dict of a map, with the keys in sorted order.
func DictOf(m map[string]any) *Dict {
	d := NewDict()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d.Set(k, m[k])
	}
	return d
}

func (d *Dict) Set(k string, v any) {
	if _, ok := d.m[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.m[k] = v
}

func (d *Dict) Get(k string) (any, bool) {
	v, ok := d.m[k]
	return v, ok
}

func (d *Dict) Keys() []string { return append([]string(nil), d.keys...) }
func (d *Dict) Len() int       { return len(d.keys) }

// Func is a function that a template calls, with its positional and keyword arguments.
type Func func(args []any, kwargs map[string]any) any

// Object is a value with attributes and items of its own, for example the printer.
type Object interface {
	// Attr returns an attribute, and false when there is none.
	Attr(name string) (any, bool)
	// Item returns an item, and false when there is none.
	Item(key any) (any, bool)
}

// Iterable is an Object that a for loop and "in" can use.
type Iterable interface {
	Iter() []any
}

// Stringer is an Object with its own str() text.
type Stringer interface {
	PyStr() string
}

// Undefined is the value of a missing name or attribute. It prints as an empty string.
type Undefined struct{ Hint string }

// Error is an error of a template: a Python exception, with its type and message.
type Error struct {
	Type, Msg string
}

func (e *Error) Error() string {
	if e.Msg == "" {
		return e.Type
	}
	return e.Type + ": " + e.Msg
}

func raise(typ, format string, args ...any) { panic(&Error{typ, fmt.Sprintf(format, args...)}) }

// typeName is the name of the type of a value, as Python's type(v).__name__.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "str"
	case []any:
		return "list"
	case Tuple:
		return "tuple"
	case *Dict:
		return "dict"
	case Func:
		return "function"
	case *Undefined:
		return "Undefined"
	}
	return "object"
}

// Str is Python's str() of a value.
func Str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case *Undefined:
		return ""
	case Stringer:
		return x.PyStr()
	}
	return Repr(v)
}

// Repr is Python's repr() of a value.
func Repr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return pyfmt.Float(x)
	case string:
		return reprString(x)
	case []any:
		return "[" + joinRepr(x) + "]"
	case Tuple:
		if len(x) == 1 {
			return "(" + Repr(x[0]) + ",)"
		}
		return "(" + joinRepr(x) + ")"
	case *Dict:
		parts := make([]string, len(x.keys))
		for i, k := range x.keys {
			parts[i] = reprString(k) + ": " + Repr(x.m[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Undefined:
		return "Undefined"
	case Stringer:
		return x.PyStr()
	}
	return fmt.Sprint(v)
}

func joinRepr(l []any) string {
	parts := make([]string, len(l))
	for i, v := range l {
		parts[i] = Repr(v)
	}
	return strings.Join(parts, ", ")
}

// reprString is Python's repr() of a string: single quotes, unless the string has a single
// quote and no double quote.
func reprString(s string) string {
	q := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == rune(q) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(q)
	return b.String()
}

// Truthy is Python's bool() of a value.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case Tuple:
		return len(x) > 0
	case *Dict:
		return x.Len() > 0
	case *Undefined:
		return false
	case Iterable:
		return len(x.Iter()) > 0
	}
	return true
}

// norm converts the Go number types to int64 and float64.
func norm(v any) any {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float32:
		return float64(x)
	case map[string]any:
		return DictOf(x)
	case []float64:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = f
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	}
	return v
}

// number returns a number as a float, and whether it is a number (bool is a number in Python).
func number(v any) (float64, bool, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true, true
		}
		return 0, true, true
	case int64:
		return float64(x), true, true
	case float64:
		return x, false, true
	}
	return 0, false, false
}

func intOf(v any) (int64, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case int64:
		return x, true
	}
	return 0, false
}

// Equal is Python's ==.
func Equal(a, b any) bool {
	a, b = norm(a), norm(b)
	if fa, _, ok := number(a); ok {
		if fb, _, ok := number(b); ok {
			ia, aInt := intOf(a)
			ib, bInt := intOf(b)
			if aInt && bInt {
				return ia == ib
			}
			return fa == fb
		}
		return false
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		return ok && seqEqual(x, y)
	case Tuple:
		y, ok := b.(Tuple)
		return ok && seqEqual(x, y)
	case *Dict:
		y, ok := b.(*Dict)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.keys {
			yv, ok := y.m[k]
			if !ok || !Equal(x.m[k], yv) {
				return false
			}
		}
		return true
	case *Undefined:
		_, ok := b.(*Undefined)
		return ok
	}
	return a == b
}

func seqEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Compare is Python's ordering: -1, 0 or 1. Values that Python does not order are a TypeError.
func Compare(a, b any, op string) int {
	a, b = norm(a), norm(b)
	if fa, _, ok := number(a); ok {
		if fb, _, ok := number(b); ok {
			ia, aInt := intOf(a)
			ib, bInt := intOf(b)
			switch {
			case aInt && bInt:
				return cmp3(ia < ib, ia > ib)
			default:
				return cmp3(fa < fb, fa > fb)
			}
		}
	}
	switch x := a.(type) {
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y)
		}
	case []any:
		if y, ok := b.([]any); ok {
			return seqCompare(x, y, op)
		}
	case Tuple:
		if y, ok := b.(Tuple); ok {
			return seqCompare(x, y, op)
		}
	}
	raise("TypeError", "'%s' not supported between instances of '%s' and '%s'", op, typeName(a), typeName(b))
	return 0
}

func cmp3(lt, gt bool) int {
	switch {
	case lt:
		return -1
	case gt:
		return 1
	}
	return 0
}

func seqCompare(a, b []any, op string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if !Equal(a[i], b[i]) {
			return Compare(a[i], b[i], op)
		}
	}
	return cmp3(len(a) < len(b), len(a) > len(b))
}

// Contains is Python's "a in b".
func Contains(container, item any) bool {
	container, item = norm(container), norm(item)
	switch c := container.(type) {
	case string:
		s, ok := item.(string)
		if !ok {
			raise("TypeError", "'in <string>' requires string as left operand, not %s", typeName(item))
		}
		return strings.Contains(c, s)
	case []any:
		for _, v := range c {
			if Equal(v, item) {
				return true
			}
		}
		return false
	case Tuple:
		for _, v := range c {
			if Equal(v, item) {
				return true
			}
		}
		return false
	case *Dict:
		s, ok := item.(string)
		if !ok {
			return false
		}
		_, ok = c.m[s]
		return ok
	case Object:
		if _, ok := c.Item(item); ok {
			return true
		}
		if it, ok := c.(Iterable); ok {
			for _, v := range it.Iter() {
				if Equal(v, item) {
					return true
				}
			}
		}
		return false
	}
	raise("TypeError", "argument of type '%s' is not iterable", typeName(container))
	return false
}

// Iter returns the items of a value for a for loop: the items of a sequence, the keys of a dict,
// the characters of a string.
func Iter(v any) []any {
	switch x := norm(v).(type) {
	case []any:
		return x
	case Tuple:
		return x
	case *Dict:
		out := make([]any, len(x.keys))
		for i, k := range x.keys {
			out[i] = k
		}
		return out
	case string:
		var out []any
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	case *Undefined:
		return nil
	case Iterable:
		return x.Iter()
	}
	raise("TypeError", "'%s' object is not iterable", typeName(v))
	return nil
}

// floorDiv and floorMod are Python's // and % of numbers.
func floorDivInt(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorModFloat(a, b float64) float64 {
	m := math.Mod(a, b)
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}
