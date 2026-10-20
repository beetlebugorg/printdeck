package jinja

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type refCase struct {
	Template    string  `json:"template"`
	Context     string  `json:"context"`
	Output      *string `json:"output"`
	Error       *string `json:"error"`
	SyntaxError *string `json:"syntax_error"`
}

// decodeJSON reads a JSON value as the values of Python: an object is a *Dict in key order, and a
// number with a fraction or an exponent is a float.
func decodeJSON(t *testing.T, dec *json.Decoder) any {
	tok, err := dec.Token()
	if err != nil {
		t.Fatal(err)
	}
	switch x := tok.(type) {
	case json.Delim:
		switch x {
		case '{':
			d := NewDict()
			for dec.More() {
				k, _ := dec.Token()
				d.Set(k.(string), decodeJSON(t, dec))
			}
			dec.Token()
			return d
		case '[':
			l := []any{}
			for dec.More() {
				l = append(l, decodeJSON(t, dec))
			}
			dec.Token()
			return l
		}
	case json.Number:
		if strings.ContainsAny(string(x), ".eE") {
			f, _ := x.Float64()
			return f
		}
		n, _ := x.Int64()
		return n
	case string, bool, nil:
		return x
	}
	t.Fatalf("unexpected JSON token %v", tok)
	return nil
}

// The engine renders each case of testdata/cases.txt as Jinja2 does (testdata/cases.json, from
// tools/klipper-reference/dumpjinja.py): the same output, error, or syntax error.
func TestMatchesJinja2(t *testing.T) {
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []refCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.Context)))
		dec.UseNumber()
		ctx := map[string]any{}
		d := decodeJSON(t, dec).(*Dict)
		for _, k := range d.Keys() {
			ctx[k], _ = d.Get(k)
		}
		tpl, err := Parse(c.Template)
		if c.SyntaxError != nil {
			if err == nil || !strings.HasPrefix(*c.SyntaxError, err.Error()) {
				t.Errorf("case %d %q: syntax error %v, want %q", i, c.Template, err, *c.SyntaxError)
			}
			continue
		}
		if err != nil {
			t.Errorf("case %d %q: %v", i, c.Template, err)
			continue
		}
		out, err := tpl.Render(ctx)
		if c.Error != nil {
			if err == nil || err.Error() != *c.Error {
				t.Errorf("case %d %q: error %v, want %q", i, c.Template, err, *c.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("case %d %q: %v", i, c.Template, err)
			continue
		}
		if out != *c.Output {
			t.Errorf("case %d %q:\n got %q\nwant %q", i, c.Template, out, *c.Output)
		}
	}
}
