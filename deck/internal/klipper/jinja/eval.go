package jinja

import (
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"sort"
	"strings"
	"unicode"
)

// Template is a parsed template.
type Template struct {
	body []node
}

// Parse parses a template. A syntax error is a *SyntaxError.
func Parse(src string) (*Template, error) {
	body, err := parse(src)
	if err != nil {
		return nil, err
	}
	return &Template{body}, nil
}

// Render renders the template with the values of ctx. An error of the template is an *Error.
func (t *Template) Render(ctx map[string]any) (out string, err error) {
	e := &evaluator{scopes: []map[string]any{globals(), ctx}}
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *Error:
				err = x
			case pyTyped:
				err = &Error{x.PyType(), x.Error()}
			case error:
				log.Printf("template: %v\n%s", x, debug.Stack())
				err = &Error{"Exception", x.Error()}
			default:
				panic(r)
			}
		}
	}()
	var b strings.Builder
	e.renderBody(&b, t.body)
	return b.String(), nil
}

// pyTyped is an error with the name of its Python type, for the error messages of the templates.
type pyTyped interface {
	error
	PyType() string
}

type evaluator struct {
	scopes []map[string]any
}

func (e *evaluator) push() { e.scopes = append(e.scopes, map[string]any{}) }
func (e *evaluator) pop()  { e.scopes = e.scopes[:len(e.scopes)-1] }

func (e *evaluator) lookup(name string) any {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		if v, ok := e.scopes[i][name]; ok {
			return norm(v)
		}
	}
	return &Undefined{fmt.Sprintf("'%s' is undefined", name)}
}

func (e *evaluator) assign(target node, v any) {
	switch t := target.(type) {
	case *nName:
		e.scopes[len(e.scopes)-1][t.name] = v
	case *nTuple:
		items := Iter(v)
		if len(items) != len(t.items) {
			if len(items) > len(t.items) {
				raise("ValueError", "too many values to unpack (expected %d)", len(t.items))
			}
			raise("ValueError", "not enough values to unpack (expected %d, got %d)", len(t.items), len(items))
		}
		for i, it := range t.items {
			e.assign(it, items[i])
		}
	case *nGetattr:
		ns, ok := e.eval(t.obj).(*Namespace)
		if !ok {
			raise("TemplateRuntimeError", "cannot assign attribute on non-namespace object")
		}
		ns.attrs.Set(t.attr, v)
	}
}

func (e *evaluator) renderBody(b *strings.Builder, body []node) {
	for _, n := range body {
		e.renderNode(b, n)
	}
}

func (e *evaluator) renderNode(b *strings.Builder, n node) {
	switch x := n.(type) {
	case *nData:
		b.WriteString(x.text)
	case *nOutput:
		b.WriteString(Str(e.eval(x.expr)))
	case *nIf:
		if Truthy(e.eval(x.cond)) {
			e.renderBody(b, x.body)
		} else {
			e.renderBody(b, x.elseBody)
		}
	case *nFor:
		e.renderFor(b, x)
	case *nSet:
		if x.body != nil {
			var sb strings.Builder
			e.renderBody(&sb, x.body)
			e.assign(x.target, sb.String())
		} else {
			e.assign(x.target, e.eval(x.value))
		}
	}
}

func (e *evaluator) renderFor(b *strings.Builder, x *nFor) {
	items := Iter(e.eval(x.iter))
	if x.filter != nil {
		var kept []any
		for _, it := range items {
			e.push()
			e.assign(x.target, it)
			if Truthy(e.eval(x.filter)) {
				kept = append(kept, it)
			}
			e.pop()
		}
		items = kept
	}
	if len(items) == 0 {
		e.renderBody(b, x.elseBody)
		return
	}
	for i, it := range items {
		e.push()
		e.scopes[len(e.scopes)-1]["loop"] = &loopObj{items: items, i: i}
		e.assign(x.target, it)
		e.renderBody(b, x.body)
		e.pop()
	}
}

// loopObj is the loop variable of a for loop.
type loopObj struct {
	items []any
	i     int
}

func (l *loopObj) Attr(name string) (any, bool) {
	n := int64(len(l.items))
	i := int64(l.i)
	switch name {
	case "index":
		return i + 1, true
	case "index0":
		return i, true
	case "revindex":
		return n - i, true
	case "revindex0":
		return n - i - 1, true
	case "first":
		return i == 0, true
	case "last":
		return i == n-1, true
	case "length":
		return n, true
	case "depth":
		return int64(1), true
	case "depth0":
		return int64(0), true
	case "previtem":
		if i > 0 {
			return l.items[i-1], true
		}
		return &Undefined{"there is no previous item"}, true
	case "nextitem":
		if i < n-1 {
			return l.items[i+1], true
		}
		return &Undefined{"there is no next item"}, true
	case "cycle":
		return Func(func(args []any, _ map[string]any) any {
			if len(args) == 0 {
				raise("TypeError", "no items for cycling given")
			}
			return args[l.i%len(args)]
		}), true
	}
	return nil, false
}

func (l *loopObj) Item(key any) (any, bool) { return nil, false }

// Namespace is the object of namespace(): its attributes can be set in a loop.
type Namespace struct{ attrs *Dict }

func (n *Namespace) Attr(name string) (any, bool) { return n.attrs.Get(name) }
func (n *Namespace) Item(key any) (any, bool) {
	if s, ok := key.(string); ok {
		return n.attrs.Get(s)
	}
	return nil, false
}
func (n *Namespace) PyStr() string { return "<Namespace " + Repr(n.attrs) + ">" }

func globals() map[string]any {
	return map[string]any{
		"range": Func(func(args []any, _ map[string]any) any {
			var start, stop, step int64 = 0, 0, 1
			ints := make([]int64, len(args))
			for i, a := range args {
				n, ok := intOf(norm(a))
				if !ok {
					raise("TypeError", "'%s' object cannot be interpreted as an integer", typeName(norm(a)))
				}
				ints[i] = n
			}
			switch len(ints) {
			case 1:
				stop = ints[0]
			case 2:
				start, stop = ints[0], ints[1]
			case 3:
				start, stop, step = ints[0], ints[1], ints[2]
			default:
				raise("TypeError", "range expected at most 3 arguments, got %d", len(args))
			}
			if step == 0 {
				raise("ValueError", "range() arg 3 must not be zero")
			}
			out := []any{}
			for v := start; (step > 0 && v < stop) || (step < 0 && v > stop); v += step {
				out = append(out, v)
			}
			return out
		}),
		"dict": Func(func(args []any, kwargs map[string]any) any {
			d := NewDict()
			if len(args) > 0 {
				if src, ok := norm(args[0]).(*Dict); ok {
					for _, k := range src.keys {
						d.Set(k, src.m[k])
					}
				}
			}
			for _, k := range sortedKeys(kwargs) {
				d.Set(k, kwargs[k])
			}
			return d
		}),
		"namespace": Func(func(args []any, kwargs map[string]any) any {
			d := NewDict()
			for _, k := range sortedKeys(kwargs) {
				d.Set(k, kwargs[k])
			}
			return &Namespace{d}
		}),
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func undefinedCheck(v any) {
	if u, ok := v.(*Undefined); ok {
		raise("UndefinedError", "%s", u.Hint)
	}
}

func objectTypeRepr(v any) string {
	switch v.(type) {
	case nil:
		return "None"
	case *Dict:
		return "dict object"
	case []any:
		return "list object"
	case Tuple:
		return "tuple object"
	case string:
		return "str object"
	case int64:
		return "int object"
	case float64:
		return "float object"
	case bool:
		return "bool object"
	}
	return "object"
}

// getAttr is obj.name of Jinja: the attribute, then the item, then undefined.
func getAttr(obj any, name string) any {
	obj = norm(obj)
	undefinedCheck(obj)
	if m := method(obj, name); m != nil {
		return m
	}
	switch x := obj.(type) {
	case *Dict:
		if v, ok := x.Get(name); ok {
			return v
		}
	case Object:
		if v, ok := x.Attr(name); ok {
			return norm(v)
		}
		if v, ok := x.Item(name); ok {
			return norm(v)
		}
	}
	return &Undefined{fmt.Sprintf("'%s' has no attribute '%s'", objectTypeRepr(obj), name)}
}

// getItem is obj[key] of Jinja: the item, then the attribute, then undefined.
func getItem(obj any, key any) any {
	obj, key = norm(obj), norm(key)
	undefinedCheck(obj)
	switch x := obj.(type) {
	case *Dict:
		if s, ok := key.(string); ok {
			if v, ok := x.Get(s); ok {
				return v
			}
		}
	case []any, Tuple, string:
		var seq []any
		var str []rune
		switch y := x.(type) {
		case []any:
			seq = y
		case Tuple:
			seq = y
		case string:
			str = []rune(y)
		}
		n := len(seq)
		if str != nil {
			n = len(str)
		}
		if sl, ok := key.(sliceKey); ok {
			idx := sliceIndexes(sl, n)
			if str != nil {
				var b strings.Builder
				for _, i := range idx {
					b.WriteRune(str[i])
				}
				return b.String()
			}
			out := make([]any, len(idx))
			for j, i := range idx {
				out[j] = seq[i]
			}
			if _, isTuple := x.(Tuple); isTuple {
				return Tuple(out)
			}
			return out
		}
		if i, ok := intOf(key); ok {
			if i < 0 {
				i += int64(n)
			}
			if i >= 0 && i < int64(n) {
				if str != nil {
					return string(str[i])
				}
				return seq[i]
			}
		}
	case Object:
		if v, ok := x.Item(key); ok {
			return norm(v)
		}
	}
	if s, ok := key.(string); ok {
		if m := method(obj, s); m != nil {
			return m
		}
		if o, ok := obj.(Object); ok {
			if v, ok := o.Attr(s); ok {
				return norm(v)
			}
		}
		return &Undefined{fmt.Sprintf("'%s' has no attribute '%s'", objectTypeRepr(obj), s)}
	}
	return &Undefined{fmt.Sprintf("%s has no element %s", objectTypeRepr(obj), Repr(key))}
}

type sliceKey struct {
	start, stop, step any
}

func sliceIndexes(s sliceKey, n int) []int {
	step := int64(1)
	if s.step != nil {
		step, _ = intOf(norm(s.step))
		if step == 0 {
			raise("ValueError", "slice step cannot be zero")
		}
	}
	clamp := func(v any, def int64) int64 {
		if v == nil {
			return def
		}
		i, _ := intOf(norm(v))
		if i < 0 {
			i += int64(n)
		}
		if step > 0 {
			return max(0, min(int64(n), i))
		}
		return max(-1, min(int64(n)-1, i))
	}
	var start, stop int64
	if step > 0 {
		start, stop = clamp(s.start, 0), clamp(s.stop, int64(n))
	} else {
		start, stop = clamp(s.start, int64(n)-1), clamp(s.stop, -1)
	}
	var out []int
	for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
		out = append(out, int(i))
	}
	return out
}

func (e *evaluator) evalList(ns []node) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = e.eval(n)
	}
	return out
}

func (e *evaluator) evalKwargs(ks []kwarg) map[string]any {
	out := map[string]any{}
	for _, k := range ks {
		out[k.name] = e.eval(k.v)
	}
	return out
}

func (e *evaluator) eval(n node) any {
	switch x := n.(type) {
	case *nConst:
		return x.v
	case *nName:
		return e.lookup(x.name)
	case *nTuple:
		return Tuple(e.evalList(x.items))
	case *nList:
		return e.evalList(x.items)
	case *nDict:
		d := NewDict()
		for i, k := range x.keys {
			ks, ok := e.eval(k).(string)
			if !ok {
				raise("TypeError", "dict keys must be strings here")
			}
			d.Set(ks, e.eval(x.values[i]))
		}
		return d
	case *nGetattr:
		return getAttr(e.eval(x.obj), x.attr)
	case *nGetitem:
		if s, ok := x.key.(*nSlice); ok {
			var sk sliceKey
			if s.start != nil {
				sk.start = e.eval(s.start)
			}
			if s.stop != nil {
				sk.stop = e.eval(s.stop)
			}
			if s.step != nil {
				sk.step = e.eval(s.step)
			}
			return getItem(e.eval(x.obj), sk)
		}
		return getItem(e.eval(x.obj), e.eval(x.key))
	case *nCall:
		fn := e.eval(x.fn)
		undefinedCheck(fn)
		f, ok := fn.(Func)
		if !ok {
			raise("TypeError", "'%s' object is not callable", typeName(fn))
		}
		return norm(f(e.evalList(x.args), e.evalKwargs(x.kwargs)))
	case *nFilter:
		return norm(filters[x.name](e.eval(x.expr), e.evalList(x.args), e.evalKwargs(x.kwargs)))
	case *nTest:
		r := tests[x.name](e.eval(x.expr), e.evalList(x.args))
		return r != x.negate
	case *nUnary:
		v := e.eval(x.expr)
		switch x.op {
		case "not":
			return !Truthy(v)
		case "-":
			undefinedCheck(v)
			switch y := v.(type) {
			case int64:
				return -y
			case bool:
				if y {
					return int64(-1)
				}
				return int64(0)
			case float64:
				return -y
			}
			raise("TypeError", "bad operand type for unary -: '%s'", typeName(v))
		case "+":
			undefinedCheck(v)
			if _, _, ok := number(v); !ok {
				raise("TypeError", "bad operand type for unary +: '%s'", typeName(v))
			}
			if b, ok := v.(bool); ok {
				n, _ := intOf(b)
				return n
			}
			return v
		}
	case *nBinary:
		switch x.op {
		case "and":
			l := e.eval(x.left)
			if !Truthy(l) {
				return l
			}
			return e.eval(x.right)
		case "or":
			l := e.eval(x.left)
			if Truthy(l) {
				return l
			}
			return e.eval(x.right)
		}
		return binaryOp(x.op, e.eval(x.left), e.eval(x.right))
	case *nConcat:
		var b strings.Builder
		for _, it := range x.items {
			b.WriteString(Str(e.eval(it)))
		}
		return b.String()
	case *nCompare:
		left := e.eval(x.first)
		for i, op := range x.ops {
			right := e.eval(x.rest[i])
			if !compareOp(op, left, right) {
				return false
			}
			left = right
		}
		return true
	case *nCondExpr:
		if Truthy(e.eval(x.test)) {
			return e.eval(x.then)
		}
		if x.otherwise == nil {
			return &Undefined{"the inline if-expression on line evaluated to false and no else section was defined."}
		}
		return e.eval(x.otherwise)
	}
	raise("TemplateRuntimeError", "unknown node %T", n)
	return nil
}

func compareOp(op string, a, b any) bool {
	switch op {
	case "==":
		return Equal(a, b)
	case "!=":
		return !Equal(a, b)
	case "in":
		return Contains(b, a)
	case "notin":
		return !Contains(b, a)
	}
	undefinedCheck(a)
	undefinedCheck(b)
	c := Compare(a, b, op)
	switch op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	}
	return false
}

var pyOp = map[string]string{"+": "+", "-": "-", "*": "*", "/": "/", "//": "//", "%": "%", "**": "** or pow()"}

func binaryOp(op string, a, b any) any {
	a, b = norm(a), norm(b)
	undefinedCheck(a)
	undefinedCheck(b)
	unsupported := func() {
		raise("TypeError", "unsupported operand type(s) for %s: '%s' and '%s'", pyOp[op], typeName(a), typeName(b))
	}
	// Sequences and strings
	switch x := a.(type) {
	case string:
		switch op {
		case "+":
			if y, ok := b.(string); ok {
				return x + y
			}
			raise("TypeError", "can only concatenate str (not \"%s\") to str", typeName(b))
		case "*":
			if n, ok := intOf(b); ok {
				return strings.Repeat(x, int(max(0, n)))
			}
			raise("TypeError", "can't multiply sequence by non-int of type '%s'", typeName(b))
		case "%":
			return PercentFormat(x, b)
		}
		unsupported()
	case []any:
		switch op {
		case "+":
			if y, ok := b.([]any); ok {
				return append(append([]any{}, x...), y...)
			}
			raise("TypeError", "can only concatenate list (not \"%s\") to list", typeName(b))
		case "*":
			if n, ok := intOf(b); ok {
				var out []any
				for i := int64(0); i < n; i++ {
					out = append(out, x...)
				}
				if out == nil {
					out = []any{}
				}
				return out
			}
		}
		unsupported()
	case Tuple:
		if op == "+" {
			if y, ok := b.(Tuple); ok {
				return append(append(Tuple{}, x...), y...)
			}
		}
		unsupported()
	}
	if s, ok := b.(string); ok && op == "*" {
		if n, ok := intOf(a); ok {
			return strings.Repeat(s, int(max(0, n)))
		}
	}
	fa, _, okA := number(a)
	fb, _, okB := number(b)
	if !okA || !okB {
		unsupported()
	}
	ia, aInt := intOf(a)
	ib, bInt := intOf(b)
	if aInt && bInt {
		switch op {
		case "+":
			return ia + ib
		case "-":
			return ia - ib
		case "*":
			return ia * ib
		case "/":
			if ib == 0 {
				raise("ZeroDivisionError", "division by zero")
			}
			return float64(ia) / float64(ib)
		case "//":
			if ib == 0 {
				raise("ZeroDivisionError", "integer division or modulo by zero")
			}
			return floorDivInt(ia, ib)
		case "%":
			if ib == 0 {
				raise("ZeroDivisionError", "integer modulo by zero")
			}
			return ia - floorDivInt(ia, ib)*ib
		case "**":
			if ib < 0 {
				return math.Pow(fa, fb)
			}
			r := int64(1)
			for i := int64(0); i < ib; i++ {
				r *= ia
			}
			return r
		}
	}
	switch op {
	case "+":
		return fa + fb
	case "-":
		return fa - fb
	case "*":
		return fa * fb
	case "/":
		if fb == 0 {
			raise("ZeroDivisionError", "float division by zero")
		}
		return fa / fb
	case "//":
		if fb == 0 {
			raise("ZeroDivisionError", "float floor division by zero")
		}
		return math.Floor(fa / fb)
	case "%":
		if fb == 0 {
			raise("ZeroDivisionError", "float modulo by zero")
		}
		return floorModFloat(fa, fb)
	case "**":
		if fa == 0 && fb < 0 {
			raise("ZeroDivisionError", "zero to a negative power")
		}
		return math.Pow(fa, fb)
	}
	unsupported()
	return nil
}

// Methods of the values

func bound(f func(args []any, kwargs map[string]any) any) any { return Func(f) }

func argStr(args []any, i int, def string) string {
	if i < len(args) {
		if s, ok := norm(args[i]).(string); ok {
			return s
		}
		if args[i] == nil {
			return def
		}
		raise("TypeError", "must be str, not %s", typeName(norm(args[i])))
	}
	return def
}

func method(obj any, name string) any {
	switch x := obj.(type) {
	case string:
		return stringMethod(x, name)
	case *Dict:
		switch name {
		case "get":
			return bound(func(args []any, _ map[string]any) any {
				var def any
				if len(args) > 1 {
					def = args[1]
				}
				if k, ok := norm(args[0]).(string); ok {
					if v, ok := x.Get(k); ok {
						return v
					}
				}
				return def
			})
		case "keys":
			return bound(func([]any, map[string]any) any {
				out := []any{}
				for _, k := range x.keys {
					out = append(out, k)
				}
				return out
			})
		case "values":
			return bound(func([]any, map[string]any) any {
				out := []any{}
				for _, k := range x.keys {
					out = append(out, x.m[k])
				}
				return out
			})
		case "items":
			return bound(func([]any, map[string]any) any {
				out := []any{}
				for _, k := range x.keys {
					out = append(out, Tuple{k, x.m[k]})
				}
				return out
			})
		case "copy":
			return bound(func([]any, map[string]any) any {
				d := NewDict()
				for _, k := range x.keys {
					d.Set(k, x.m[k])
				}
				return d
			})
		case "update":
			return bound(func(args []any, kwargs map[string]any) any {
				if len(args) > 0 {
					if src, ok := norm(args[0]).(*Dict); ok {
						for _, k := range src.keys {
							x.Set(k, src.m[k])
						}
					}
				}
				for _, k := range sortedKeys(kwargs) {
					x.Set(k, kwargs[k])
				}
				return nil
			})
		}
	case []any:
		switch name {
		case "index":
			return bound(func(args []any, _ map[string]any) any {
				for i, v := range x {
					if Equal(v, args[0]) {
						return int64(i)
					}
				}
				raise("ValueError", "%s is not in list", Repr(args[0]))
				return nil
			})
		case "count":
			return bound(func(args []any, _ map[string]any) any {
				n := int64(0)
				for _, v := range x {
					if Equal(v, args[0]) {
						n++
					}
				}
				return n
			})
		}
	}
	return nil
}

func stringMethod(s, name string) any {
	switch name {
	case "strip", "lstrip", "rstrip":
		return bound(func(args []any, _ map[string]any) any {
			chars := argStr(args, 0, "")
			trim := func(r rune) bool {
				if chars == "" {
					return unicode.IsSpace(r)
				}
				return strings.ContainsRune(chars, r)
			}
			switch name {
			case "lstrip":
				return strings.TrimLeftFunc(s, trim)
			case "rstrip":
				return strings.TrimRightFunc(s, trim)
			}
			return strings.TrimFunc(s, trim)
		})
	case "lower":
		return bound(func([]any, map[string]any) any { return strings.ToLower(s) })
	case "upper":
		return bound(func([]any, map[string]any) any { return strings.ToUpper(s) })
	case "title":
		return bound(func([]any, map[string]any) any { return pyTitle(s) })
	case "capitalize":
		return bound(func([]any, map[string]any) any { return pyCapitalize(s) })
	case "split", "rsplit":
		return bound(func(args []any, kwargs map[string]any) any {
			sep := argStr(args, 0, "")
			maxsplit := int64(-1)
			if len(args) > 1 {
				maxsplit, _ = intOf(norm(args[1]))
			}
			var parts []string
			if sep == "" {
				parts = strings.Fields(s)
				if maxsplit >= 0 && int64(len(parts)) > maxsplit+1 {
					// Python keeps the rest of the string after maxsplit splits.
					rest := s
					parts = nil
					for i := int64(0); i < maxsplit; i++ {
						rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
						j := strings.IndexFunc(rest, unicode.IsSpace)
						parts = append(parts, rest[:j])
						rest = rest[j:]
					}
					parts = append(parts, strings.TrimLeftFunc(rest, unicode.IsSpace))
				}
			} else if maxsplit >= 0 {
				parts = strings.SplitN(s, sep, int(maxsplit)+1)
			} else {
				parts = strings.Split(s, sep)
			}
			out := make([]any, len(parts))
			for i, p := range parts {
				out[i] = p
			}
			return out
		})
	case "startswith", "endswith":
		return bound(func(args []any, _ map[string]any) any {
			prefixes := []string{}
			switch a := norm(args[0]).(type) {
			case string:
				prefixes = append(prefixes, a)
			case Tuple:
				for _, p := range a {
					prefixes = append(prefixes, Str(p))
				}
			}
			for _, p := range prefixes {
				if (name == "startswith" && strings.HasPrefix(s, p)) || (name == "endswith" && strings.HasSuffix(s, p)) {
					return true
				}
			}
			return false
		})
	case "replace":
		return bound(func(args []any, _ map[string]any) any {
			n := int64(-1)
			if len(args) > 2 {
				n, _ = intOf(norm(args[2]))
			}
			return strings.Replace(s, argStr(args, 0, ""), argStr(args, 1, ""), int(n))
		})
	case "find", "rfind":
		return bound(func(args []any, _ map[string]any) any {
			sub := argStr(args, 0, "")
			var i int
			if name == "find" {
				i = strings.Index(s, sub)
			} else {
				i = strings.LastIndex(s, sub)
			}
			if i < 0 {
				return int64(-1)
			}
			return int64(len([]rune(s[:i])))
		})
	case "count":
		return bound(func(args []any, _ map[string]any) any {
			return int64(strings.Count(s, argStr(args, 0, "")))
		})
	case "join":
		return bound(func(args []any, _ map[string]any) any {
			items := Iter(args[0])
			parts := make([]string, len(items))
			for i, it := range items {
				str, ok := norm(it).(string)
				if !ok {
					raise("TypeError", "sequence item %d: expected str instance, %s found", i, typeName(norm(it)))
				}
				parts[i] = str
			}
			return strings.Join(parts, s)
		})
	case "format":
		return bound(func(args []any, kwargs map[string]any) any { return StrFormat(s, args, kwargs) })
	case "isdigit", "isnumeric", "isdecimal":
		return bound(func([]any, map[string]any) any {
			if s == "" {
				return false
			}
			for _, r := range s {
				if !unicode.IsDigit(r) {
					return false
				}
			}
			return true
		})
	case "isalpha":
		return bound(func([]any, map[string]any) any {
			if s == "" {
				return false
			}
			for _, r := range s {
				if !unicode.IsLetter(r) {
					return false
				}
			}
			return true
		})
	case "isspace":
		return bound(func([]any, map[string]any) any {
			return s != "" && strings.TrimFunc(s, unicode.IsSpace) == ""
		})
	case "zfill":
		return bound(func(args []any, _ map[string]any) any {
			w, _ := intOf(norm(args[0]))
			sign := ""
			body := s
			if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
				sign, body = s[:1], s[1:]
			}
			for int64(len(sign)+len(body)) < w {
				body = "0" + body
			}
			return sign + body
		})
	case "center", "ljust", "rjust":
		return bound(func(args []any, _ map[string]any) any {
			w, _ := intOf(norm(args[0]))
			fill := argStr(args, 1, " ")
			align := map[string]byte{"center": '^', "ljust": '<', "rjust": '>'}[name]
			sp := spec{fill: []rune(fill)[0], align: align, width: int(w)}
			if align == '^' {
				// str.center puts the extra fill on the left when the padding is odd and the width
				// is odd, as Python does.
				n := len([]rune(s))
				padding := int(w) - n
				if padding <= 0 {
					return s
				}
				left := padding / 2
				if padding%2 == 1 && int(w)%2 == 1 {
					left++
				}
				return strings.Repeat(fill, left) + s + strings.Repeat(fill, padding-left)
			}
			return pad(s, "", sp, align)
		})
	case "splitlines":
		return bound(func([]any, map[string]any) any {
			out := []any{}
			for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
				out = append(out, l)
			}
			if s == "" {
				return []any{}
			}
			return out
		})
	}
	return nil
}

func pyTitle(s string) string {
	var b strings.Builder
	prevLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			if prevLetter {
				b.WriteRune(unicode.ToLower(r))
			} else {
				b.WriteRune(unicode.ToUpper(r))
			}
			prevLetter = true
		} else {
			b.WriteRune(r)
			prevLetter = false
		}
	}
	return b.String()
}

func pyCapitalize(s string) string {
	r := []rune(strings.ToLower(s))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
