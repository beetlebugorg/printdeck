package jinja

import (
	"reflect"
	"testing"
)

func TestLiteralEval(t *testing.T) {
	for src, want := range map[string]any{
		"1": int64(1), "-2.5": -2.5, "'a'": "a", `"b"`: "b", "True": true, "None": nil,
		"[1, 'x']": []any{int64(1), "x"}, "(1, 2)": Tuple{int64(1), int64(2)}, "()": Tuple{},
	} {
		got, err := LiteralEval(src)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %#v %v", src, got, err)
		}
	}
	d, err := LiteralEval("{'a': 1, 'b': [2]}")
	if err != nil || Repr(d) != "{'a': 1, 'b': [2]}" {
		t.Errorf("dict: %v %v", d, err)
	}
	for _, bad := range []string{"true", "x", "1 +", "f(1)", "[1", "-'a'"} {
		if _, err := LiteralEval(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestJSONMatchesPython(t *testing.T) {
	for _, c := range jsonCases {
		v, err := JSONLoads(c[0])
		if err != nil {
			t.Errorf("JSONLoads(%s): %v", c[0], err)
			continue
		}
		if got := JSONDumps(v); got != c[1] {
			t.Errorf("JSONDumps(JSONLoads(%s)) = %s, want %s", c[0], got, c[1])
		}
	}
}
