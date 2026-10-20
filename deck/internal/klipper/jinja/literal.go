package jinja

import (
	"fmt"
)

// LiteralError is an error of LiteralEval. Type is the Python exception: "SyntaxError" or
// "ValueError". The message is Msg only, as Python's str() of the exception.
type LiteralError struct{ Type, Msg string }

func (e *LiteralError) Error() string { return e.Msg }

// LiteralEval reads a Python literal, as Python's ast.literal_eval: a number, a string, True,
// False, None, or a list, tuple, or dict of literals. The macro variables of Klipper use it. An
// error is a *LiteralError.
func LiteralEval(src string) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *SyntaxError:
				err = &LiteralError{"SyntaxError", fmt.Sprintf("invalid syntax (<unknown>, line %d)", x.Line)}
			case *Error:
				err = &LiteralError{x.Type, x.Msg}
			default:
				panic(r)
			}
		}
	}()
	// The space keeps a "-" at the start from being the whitespace control of the tag.
	toks := lex("{ " + src + " }")
	// Python spells the constants True, False and None.
	for _, t := range toks {
		if t.typ == tName && t.val != "True" && t.val != "False" && t.val != "None" {
			return nil, &LiteralError{"ValueError", "malformed node or string: " + t.val}
		}
	}
	p := &parser{toks: toks}
	if p.cur().typ != tVarBegin {
		return nil, &LiteralError{"SyntaxError", "invalid syntax"}
	}
	p.next()
	n := p.parseTuple(false)
	if p.cur().typ != tVarEnd || p.peek().typ != tEOF {
		return nil, &LiteralError{"SyntaxError", "invalid syntax"}
	}
	return literalValue(n), nil
}

func literalValue(n node) any {
	switch x := n.(type) {
	case *nConst:
		return x.v
	case *nList:
		out := []any{}
		for _, it := range x.items {
			out = append(out, literalValue(it))
		}
		return out
	case *nTuple:
		out := Tuple{}
		for _, it := range x.items {
			out = append(out, literalValue(it))
		}
		return out
	case *nDict:
		d := NewDict()
		for i, k := range x.keys {
			ks, ok := literalValue(k).(string)
			if !ok {
				raise("ValueError", "dict keys must be strings")
			}
			d.Set(ks, literalValue(x.values[i]))
		}
		return d
	case *nUnary:
		v := literalValue(x.expr)
		if _, isBool := v.(bool); !isBool {
			if _, _, ok := number(v); ok {
				if x.op == "-" {
					if f, isFloat := v.(float64); isFloat {
						return -f
					}
					i, _ := intOf(v)
					return -i
				}
				return v
			}
		}
	}
	raise("ValueError", "malformed node or string")
	return nil
}
