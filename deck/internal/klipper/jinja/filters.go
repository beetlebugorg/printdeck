package jinja

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"printdeck/internal/klipper/pyfmt"
)

// The filters and tests of Jinja2 that the templates use.

type filterFunc func(v any, args []any, kwargs map[string]any) any

func arg(args []any, kwargs map[string]any, i int, name string, def any) any {
	if i < len(args) {
		return norm(args[i])
	}
	if v, ok := kwargs[name]; ok {
		return norm(v)
	}
	return def
}

// toFloat is Python's float() of a value, with false when it fails.
func toFloat(v any) (float64, bool) {
	switch x := norm(v).(type) {
	case string:
		f, err := pyfmt.ParseFloat(x)
		return f, err == nil
	case *Undefined:
		return 0, false
	}
	f, _, ok := number(norm(v))
	return f, ok
}

// toInt is the int filter of Jinja2: int(), then int(float()).
func toInt(v any, base int64) (int64, bool) {
	switch x := norm(v).(type) {
	case string:
		if base != 10 {
			s := strings.ToLower(strings.TrimSpace(x))
			for _, p := range []string{"0x", "0o", "0b"} {
				s = strings.TrimPrefix(s, p)
			}
			n, err := strconv.ParseInt(s, int(base), 64)
			if err == nil {
				return n, true
			}
		} else if n, err := pyfmt.ParseInt(x); err == nil {
			return n, true
		}
		if f, err := pyfmt.ParseFloat(x); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return int64(f), true
		}
		return 0, false
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return 0, false
		}
		return int64(x), true
	}
	return intOf(norm(v))
}

// pyRound is Python's round(x, n): to the nearest multiple of 10**-n, with ties to even.
func pyRound(x float64, n int64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	if n >= 0 {
		s := strconv.FormatFloat(x, 'f', int(n), 64)
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	p := math.Pow(10, float64(-n))
	return math.RoundToEven(x/p) * p
}

func length(v any) int64 {
	switch x := norm(v).(type) {
	case string:
		return int64(len([]rune(x)))
	case []any:
		return int64(len(x))
	case Tuple:
		return int64(len(x))
	case *Dict:
		return int64(x.Len())
	case *Undefined:
		return 0
	case Iterable:
		return int64(len(x.Iter()))
	}
	raise("TypeError", "object of type '%s' has no len()", typeName(v))
	return 0
}

func minMax(v any, args []any, kwargs map[string]any, wantMax bool) any {
	items := Iter(v)
	if len(items) == 0 {
		return &Undefined{"No aggregated item, sequence was empty."}
	}
	caseSensitive := Truthy(arg(args, kwargs, 0, "case_sensitive", false))
	attribute := arg(args, kwargs, 1, "attribute", nil)
	key := func(it any) any {
		if attribute != nil {
			it = getItem(it, attribute)
		}
		if s, ok := it.(string); ok && !caseSensitive {
			return strings.ToLower(s)
		}
		return it
	}
	best := items[0]
	for _, it := range items[1:] {
		c := Compare(key(it), key(best), "<")
		if (wantMax && c > 0) || (!wantMax && c < 0) {
			best = it
		}
	}
	return best
}

var filters map[string]filterFunc

func init() {
	filters = map[string]filterFunc{
		"default": func(v any, args []any, kwargs map[string]any) any {
			def := arg(args, kwargs, 0, "default_value", "")
			boolean := Truthy(arg(args, kwargs, 1, "boolean", false))
			if _, ok := v.(*Undefined); ok || (boolean && !Truthy(v)) {
				return def
			}
			return v
		},
		"float": func(v any, args []any, kwargs map[string]any) any {
			if f, ok := toFloat(v); ok {
				return f
			}
			return arg(args, kwargs, 0, "default", 0.0)
		},
		"int": func(v any, args []any, kwargs map[string]any) any {
			base, _ := intOf(arg(args, kwargs, 1, "base", int64(10)))
			if n, ok := toInt(v, base); ok {
				return n
			}
			return arg(args, kwargs, 0, "default", int64(0))
		},
		"abs": func(v any, _ []any, _ map[string]any) any {
			v = norm(v)
			undefinedCheck(v)
			switch x := v.(type) {
			case int64:
				if x < 0 {
					return -x
				}
				return x
			case float64:
				return math.Abs(x)
			case bool:
				n, _ := intOf(x)
				return n
			}
			raise("TypeError", "bad operand type for abs(): '%s'", typeName(v))
			return nil
		},
		"round": func(v any, args []any, kwargs map[string]any) any {
			precision, _ := intOf(arg(args, kwargs, 0, "precision", int64(0)))
			method := Str(arg(args, kwargs, 1, "method", "common"))
			v = norm(v)
			undefinedCheck(v)
			f, _, ok := number(v)
			if !ok {
				raise("TypeError", "type %s doesn't define __round__ method", typeName(v))
			}
			switch method {
			case "common":
				if i, isInt := intOf(v); isInt {
					if precision >= 0 {
						return i
					}
					return int64(pyRound(float64(i), precision))
				}
				return pyRound(f, precision)
			case "ceil", "floor":
				p := math.Pow(10, float64(precision))
				if method == "ceil" {
					return math.Ceil(f*p) / p
				}
				return math.Floor(f*p) / p
			}
			raise("FilterArgumentError", "method must be common, ceil or floor")
			return nil
		},
		"min":    func(v any, args []any, kwargs map[string]any) any { return minMax(v, args, kwargs, false) },
		"max":    func(v any, args []any, kwargs map[string]any) any { return minMax(v, args, kwargs, true) },
		"length": func(v any, _ []any, _ map[string]any) any { return length(v) },
		"string": func(v any, _ []any, _ map[string]any) any { return Str(v) },
		"lower":  func(v any, _ []any, _ map[string]any) any { return strings.ToLower(Str(v)) },
		"upper":  func(v any, _ []any, _ map[string]any) any { return strings.ToUpper(Str(v)) },
		"capitalize": func(v any, _ []any, _ map[string]any) any {
			return pyCapitalize(Str(v))
		},
		"title": func(v any, _ []any, _ map[string]any) any { return pyTitle(Str(v)) },
		"trim": func(v any, args []any, kwargs map[string]any) any {
			chars := arg(args, kwargs, 0, "chars", nil)
			if chars == nil {
				return strings.TrimFunc(Str(v), unicode.IsSpace)
			}
			return strings.Trim(Str(v), Str(chars))
		},
		"replace": func(v any, args []any, kwargs map[string]any) any {
			n, _ := intOf(arg(args, kwargs, 2, "count", int64(-1)))
			return strings.Replace(Str(v), Str(arg(args, kwargs, 0, "old", "")), Str(arg(args, kwargs, 1, "new", "")), int(n))
		},
		"join": func(v any, args []any, kwargs map[string]any) any {
			sep := Str(arg(args, kwargs, 0, "d", ""))
			attribute := arg(args, kwargs, 1, "attribute", nil)
			items := Iter(v)
			parts := make([]string, len(items))
			for i, it := range items {
				if attribute != nil {
					it = getItem(it, attribute)
				}
				parts[i] = Str(it)
			}
			return strings.Join(parts, sep)
		},
		"list": func(v any, _ []any, _ map[string]any) any { return append([]any{}, Iter(v)...) },
		"first": func(v any, _ []any, _ map[string]any) any {
			items := Iter(v)
			if len(items) == 0 {
				return &Undefined{"No first item, sequence was empty."}
			}
			return items[0]
		},
		"last": func(v any, _ []any, _ map[string]any) any {
			items := Iter(v)
			if len(items) == 0 {
				return &Undefined{"No last item, sequence was empty."}
			}
			return items[len(items)-1]
		},
		"sum": func(v any, args []any, kwargs map[string]any) any {
			attribute := arg(args, kwargs, 0, "attribute", nil)
			var total any = arg(args, kwargs, 1, "start", int64(0))
			for _, it := range Iter(v) {
				if attribute != nil {
					it = getItem(it, attribute)
				}
				total = binaryOp("+", total, it)
			}
			return total
		},
		"reverse": func(v any, _ []any, _ map[string]any) any {
			if s, ok := norm(v).(string); ok {
				r := []rune(s)
				for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
					r[i], r[j] = r[j], r[i]
				}
				return string(r)
			}
			items := Iter(v)
			out := make([]any, len(items))
			for i, it := range items {
				out[len(items)-1-i] = it
			}
			return out
		},
		"sort": func(v any, args []any, kwargs map[string]any) any {
			reverse := Truthy(arg(args, kwargs, 0, "reverse", false))
			caseSensitive := Truthy(arg(args, kwargs, 1, "case_sensitive", false))
			attribute := arg(args, kwargs, 2, "attribute", nil)
			items := append([]any{}, Iter(v)...)
			key := func(it any) any {
				if attribute != nil {
					it = getItem(it, attribute)
				}
				if s, ok := it.(string); ok && !caseSensitive {
					return strings.ToLower(s)
				}
				return it
			}
			sort.SliceStable(items, func(i, j int) bool {
				c := Compare(key(items[i]), key(items[j]), "<")
				if reverse {
					return c > 0
				}
				return c < 0
			})
			return items
		},
		"unique": func(v any, _ []any, _ map[string]any) any {
			var out []any
			for _, it := range Iter(v) {
				dup := false
				for _, o := range out {
					if Equal(o, it) {
						dup = true
						break
					}
				}
				if !dup {
					out = append(out, it)
				}
			}
			if out == nil {
				out = []any{}
			}
			return out
		},
		"format": func(v any, args []any, kwargs map[string]any) any {
			if len(kwargs) > 0 {
				return PercentFormat(Str(v), DictOf(kwargs))
			}
			return PercentFormat(Str(v), Tuple(args))
		},
		"center": func(v any, args []any, kwargs map[string]any) any {
			w := arg(args, kwargs, 0, "width", int64(80))
			return stringMethod(Str(v), "center").(Func)([]any{w}, nil)
		},
		"dictsort": func(v any, _ []any, _ map[string]any) any {
			d, ok := norm(v).(*Dict)
			if !ok {
				raise("FilterArgumentError", "You can only sort by either \"key\" or \"value\"")
			}
			keys := d.Keys()
			sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
			out := make([]any, len(keys))
			for i, k := range keys {
				out[i] = Tuple{k, d.m[k]}
			}
			return out
		},
		"wordcount": func(v any, _ []any, _ map[string]any) any { return int64(len(strings.Fields(Str(v)))) },
	}
	filters["d"] = filters["default"]
	filters["count"] = filters["length"]
}

type testFunc func(v any, args []any) bool

var tests map[string]testFunc

func init() {
	cmp := func(op string) testFunc {
		return func(v any, args []any) bool { return compareOp(op, norm(v), norm(args[0])) }
	}
	tests = map[string]testFunc{
		"defined":   func(v any, _ []any) bool { _, u := v.(*Undefined); return !u },
		"undefined": func(v any, _ []any) bool { _, u := v.(*Undefined); return u },
		"none":      func(v any, _ []any) bool { return v == nil },
		"boolean":   func(v any, _ []any) bool { _, ok := v.(bool); return ok },
		"true":      func(v any, _ []any) bool { return v == true },
		"false":     func(v any, _ []any) bool { return v == false },
		"integer":   func(v any, _ []any) bool { _, ok := norm(v).(int64); return ok },
		"float":     func(v any, _ []any) bool { _, ok := norm(v).(float64); return ok },
		"number": func(v any, _ []any) bool {
			_, _, ok := number(norm(v))
			return ok
		},
		"string":  func(v any, _ []any) bool { _, ok := norm(v).(string); return ok },
		"mapping": func(v any, _ []any) bool { _, ok := norm(v).(*Dict); return ok },
		"iterable": func(v any, _ []any) bool {
			switch norm(v).(type) {
			case string, []any, Tuple, *Dict, Iterable:
				return true
			}
			return false
		},
		"sequence": func(v any, _ []any) bool {
			switch norm(v).(type) {
			case string, []any, Tuple, *Dict:
				return true
			}
			return false
		},
		"callable": func(v any, _ []any) bool { _, ok := v.(Func); return ok },
		"even": func(v any, _ []any) bool {
			n, ok := intOf(norm(v))
			return ok && n%2 == 0
		},
		"odd": func(v any, _ []any) bool {
			n, ok := intOf(norm(v))
			return ok && n%2 != 0
		},
		"divisibleby": func(v any, args []any) bool {
			r := binaryOp("%", v, args[0])
			return Equal(r, int64(0))
		},
		"lower": func(v any, _ []any) bool { s := Str(v); return s == strings.ToLower(s) },
		"upper": func(v any, _ []any) bool { s := Str(v); return s == strings.ToUpper(s) },
		"in":    func(v any, args []any) bool { return Contains(args[0], v) },
		"sameas": func(v any, args []any) bool {
			return v == args[0]
		},
		"eq": cmp("=="), "ne": cmp("!="), "lt": cmp("<"), "le": cmp("<="), "gt": cmp(">"), "ge": cmp(">="),
	}
	for alias, name := range map[string]string{"equalto": "eq", "==": "eq", "!=": "ne", "<": "lt",
		"lessthan": "lt", "<=": "le", ">": "gt", "greaterthan": "gt", ">=": "ge"} {
		tests[alias] = tests[name]
	}
}
