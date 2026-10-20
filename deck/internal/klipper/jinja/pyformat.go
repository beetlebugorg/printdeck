package jinja

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The formatting of Python: the % operator of strings (printf style) and str.format.

type spec struct {
	fill      rune
	align     byte // '<', '>', '^', '=' or 0
	sign      byte // '+', '-', ' ' or 0
	alt, zero bool
	width     int
	hasPrec   bool
	prec      int
	typ       byte
	grouping  byte
}

// fmtFloat formats a float with a type of e, E, f, F, g, G or % and a precision, with no sign.
func fmtFloat(v float64, typ byte, prec int, alt bool) string {
	if math.IsInf(v, 0) {
		if typ == 'E' || typ == 'F' || typ == 'G' {
			return "INF"
		}
		return "inf"
	}
	if math.IsNaN(v) {
		if typ == 'E' || typ == 'F' || typ == 'G' {
			return "NAN"
		}
		return "nan"
	}
	v = math.Abs(v)
	switch typ {
	case 'f', 'F':
		s := strconv.FormatFloat(v, 'f', prec, 64)
		if alt && prec == 0 {
			s += "."
		}
		return s
	case 'e', 'E':
		s := strconv.FormatFloat(v, 'e', prec, 64)
		if alt && prec == 0 {
			s = strings.Replace(s, "e", ".e", 1)
		}
		if typ == 'E' {
			s = strings.ToUpper(s)
		}
		return s
	case '%':
		return strconv.FormatFloat(v*100, 'f', prec, 64) + "%"
	case 'g', 'G':
		p := prec
		if p == 0 {
			p = 1
		}
		var s string
		if v == 0 {
			s = strconv.FormatFloat(0, 'f', p-1, 64)
		} else {
			e := strconv.FormatFloat(v, 'e', p-1, 64)
			exp, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
			if exp < -4 || exp >= p {
				s = e
			} else {
				s = strconv.FormatFloat(v, 'f', p-1-exp, 64)
			}
		}
		if !alt {
			s = trimZeros(s)
		}
		if typ == 'G' {
			s = strings.ToUpper(s)
		}
		return s
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// trimZeros removes the zeros at the end of the fraction, and a "." with no fraction.
func trimZeros(s string) string {
	mant, exp := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant, exp = s[:i], s[i:]
	}
	if strings.Contains(mant, ".") {
		mant = strings.TrimRight(mant, "0")
		mant = strings.TrimSuffix(mant, ".")
	}
	return mant + exp
}

// pad applies the width, the fill and the alignment. numSign is the sign of a number: the fill
// of '=' goes after it.
func pad(body, numSign string, sp spec, defAlign byte) string {
	n := utf8.RuneCountInString(body) + len(numSign)
	if n >= sp.width {
		return numSign + body
	}
	fill := sp.fill
	if fill == 0 {
		fill = ' '
	}
	align := sp.align
	if align == 0 {
		align = defAlign
	}
	padding := sp.width - n
	f := string(fill)
	switch align {
	case '<':
		return numSign + body + strings.Repeat(f, padding)
	case '^':
		left := padding / 2
		return strings.Repeat(f, left) + numSign + body + strings.Repeat(f, padding-left)
	case '=':
		return numSign + strings.Repeat(f, padding) + body
	}
	return strings.Repeat(f, padding) + numSign + body
}

func signOf(neg bool, sign byte) string {
	switch {
	case neg:
		return "-"
	case sign == '+':
		return "+"
	case sign == ' ':
		return " "
	}
	return ""
}

// groupDigits adds a separator each three digits of the integer part.
func groupDigits(s string, sep byte) string {
	intPart, rest := s, ""
	if i := strings.IndexAny(s, ".eE%"); i >= 0 {
		intPart, rest = s[:i], s[i:]
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(sep)
		}
		b.WriteRune(c)
	}
	return b.String() + rest
}

// The % operator

// PercentFormat is Python's format % args, where args is a Tuple or one value.
func PercentFormat(format string, args any) string {
	var list []any
	var mapping *Dict
	switch a := args.(type) {
	case Tuple:
		list = a
	case *Dict:
		mapping = a
		list = []any{a}
	default:
		list = []any{a}
	}
	next := 0
	arg := func() any {
		if next >= len(list) {
			raise("TypeError", "not enough arguments for format string")
		}
		v := list[next]
		next++
		return v
	}
	var b strings.Builder
	usedMapping := false
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(format) {
			raise("ValueError", "incomplete format")
		}
		var key *string
		if format[i] == '(' {
			end := strings.IndexByte(format[i:], ')')
			if end < 0 {
				raise("ValueError", "incomplete format key")
			}
			k := format[i+1 : i+end]
			key = &k
			i += end + 1
		}
		var sp spec
		for ; i < len(format) && strings.IndexByte("#0- +", format[i]) >= 0; i++ {
			switch format[i] {
			case '#':
				sp.alt = true
			case '0':
				sp.zero = true
			case '-':
				sp.align = '<'
			case ' ', '+':
				if sp.sign != '+' {
					sp.sign = format[i]
				}
			}
		}
		if i < len(format) && format[i] == '*' {
			w, _ := intOf(norm(arg()))
			sp.width = int(w)
			if sp.width < 0 {
				sp.align, sp.width = '<', -sp.width
			}
			i++
		} else {
			for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
				sp.width = sp.width*10 + int(format[i]-'0')
			}
		}
		if i < len(format) && format[i] == '.' {
			sp.hasPrec = true
			i++
			if i < len(format) && format[i] == '*' {
				p, _ := intOf(norm(arg()))
				sp.prec = int(p)
				i++
			} else {
				for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
					sp.prec = sp.prec*10 + int(format[i]-'0')
				}
			}
		}
		for i < len(format) && strings.IndexByte("hlL", format[i]) >= 0 {
			i++
		}
		if i >= len(format) {
			raise("ValueError", "incomplete format")
		}
		sp.typ = format[i]
		if sp.typ == '%' {
			b.WriteByte('%')
			continue
		}
		var v any
		if key != nil {
			if mapping == nil {
				raise("TypeError", "format requires a mapping")
			}
			usedMapping = true
			val, ok := mapping.Get(*key)
			if !ok {
				raise("KeyError", "%s", reprString(*key))
			}
			v = val
		} else {
			v = arg()
		}
		b.WriteString(percentOne(norm(v), sp))
	}
	if mapping == nil && next < len(list) {
		raise("TypeError", "not all arguments converted during string formatting")
	}
	_ = usedMapping
	return b.String()
}

func percentOne(v any, sp spec) string {
	defAlign := byte('>')
	if sp.zero && sp.align != '<' {
		sp.fill, sp.align = '0', '='
	}
	switch sp.typ {
	case 's', 'r', 'a':
		s := Str(v)
		if sp.typ != 's' {
			s = Repr(v)
		}
		if sp.hasPrec && sp.prec < utf8.RuneCountInString(s) {
			s = string([]rune(s)[:sp.prec])
		}
		if sp.align == '=' {
			sp.align, sp.fill = '>', ' '
		}
		return pad(s, "", sp, defAlign)
	case 'c':
		if s, ok := v.(string); ok {
			return pad(s, "", sp, defAlign)
		}
		n, _ := intOf(v)
		return pad(string(rune(n)), "", sp, defAlign)
	case 'd', 'i', 'u', 'x', 'X', 'o':
		var n int64
		switch x := v.(type) {
		case float64:
			if math.IsInf(x, 0) || math.IsNaN(x) {
				raise("OverflowError", "cannot convert float infinity to integer")
			}
			n = int64(x)
		default:
			var ok bool
			n, ok = intOf(x)
			if !ok {
				raise("TypeError", "%%%c format: a real number is required, not %s", sp.typ, typeName(v))
			}
		}
		neg := n < 0
		if neg {
			n = -n
		}
		var body string
		switch sp.typ {
		case 'x':
			body = strconv.FormatInt(n, 16)
			if sp.alt {
				body = "0x" + body
			}
		case 'X':
			body = strings.ToUpper(strconv.FormatInt(n, 16))
			if sp.alt {
				body = "0X" + body
			}
		case 'o':
			body = strconv.FormatInt(n, 8)
			if sp.alt {
				body = "0o" + body
			}
		default:
			body = strconv.FormatInt(n, 10)
		}
		if sp.hasPrec && len(body) < sp.prec {
			body = strings.Repeat("0", sp.prec-len(body)) + body
		}
		return pad(body, signOf(neg, sp.sign), sp, defAlign)
	case 'e', 'E', 'f', 'F', 'g', 'G':
		f, _, ok := number(v)
		if !ok {
			raise("TypeError", "must be real number, not %s", typeName(v))
		}
		if !sp.hasPrec {
			sp.prec = 6
		}
		body := fmtFloat(f, sp.typ, sp.prec, sp.alt)
		neg := math.Signbit(f) && !math.IsNaN(f)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			if sp.align == '=' {
				sp.fill = ' '
			}
		}
		return pad(body, signOf(neg, sp.sign), sp, defAlign)
	}
	raise("ValueError", "unsupported format character '%c' (0x%x)", sp.typ, sp.typ)
	return ""
}

// str.format

// parseSpec reads a format spec of str.format.
func parseSpec(s string) spec {
	var sp spec
	r := []rune(s)
	i := 0
	isAlign := func(c rune) bool { return c == '<' || c == '>' || c == '^' || c == '=' }
	if len(r) >= 2 && isAlign(r[1]) {
		sp.fill, sp.align = r[0], byte(r[1])
		i = 2
	} else if len(r) >= 1 && isAlign(r[0]) {
		sp.align = byte(r[0])
		i = 1
	}
	if i < len(r) && (r[i] == '+' || r[i] == '-' || r[i] == ' ') {
		sp.sign = byte(r[i])
		i++
	}
	if i < len(r) && r[i] == 'z' {
		i++
	}
	if i < len(r) && r[i] == '#' {
		sp.alt = true
		i++
	}
	if i < len(r) && r[i] == '0' {
		sp.zero = true
		i++
	}
	for ; i < len(r) && r[i] >= '0' && r[i] <= '9'; i++ {
		sp.width = sp.width*10 + int(r[i]-'0')
	}
	if i < len(r) && (r[i] == ',' || r[i] == '_') {
		sp.grouping = byte(r[i])
		i++
	}
	if i < len(r) && r[i] == '.' {
		sp.hasPrec = true
		i++
		for ; i < len(r) && r[i] >= '0' && r[i] <= '9'; i++ {
			sp.prec = sp.prec*10 + int(r[i]-'0')
		}
	}
	if i < len(r) {
		sp.typ = byte(r[i])
		i++
	}
	if i != len(r) {
		raise("ValueError", "Invalid format specifier '%s' for object of type 'str'", s)
	}
	return sp
}

// FormatValue is Python's format(v, spec).
func FormatValue(v any, specText string) string {
	v = norm(v)
	sp := parseSpec(specText)
	if sp.zero && sp.align == 0 {
		sp.fill, sp.align = '0', '='
	}
	switch x := v.(type) {
	case string:
		if sp.typ != 0 && sp.typ != 's' {
			raise("ValueError", "Unknown format code '%c' for object of type 'str'", sp.typ)
		}
		if sp.hasPrec && sp.prec < utf8.RuneCountInString(x) {
			x = string([]rune(x)[:sp.prec])
		}
		return pad(x, "", sp, '<')
	case bool, int64:
		n, _ := intOf(x)
		switch sp.typ {
		case 0, 'd', 'n':
			if b, isBool := x.(bool); isBool && sp.typ == 0 && specText == "" {
				return Str(b)
			}
			body := strconv.FormatInt(absInt(n), 10)
			if sp.grouping != 0 {
				body = groupDigits(body, sp.grouping)
			}
			return pad(body, signOf(n < 0, sp.sign), sp, '>')
		case 'x', 'X', 'o', 'b':
			base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'b': 2}[sp.typ]
			body := strconv.FormatInt(absInt(n), base)
			if sp.typ == 'X' {
				body = strings.ToUpper(body)
			}
			if sp.alt {
				body = "0" + string(sp.typ) + body
			}
			return pad(body, signOf(n < 0, sp.sign), sp, '>')
		case 'c':
			return pad(string(rune(n)), "", sp, '<')
		}
		return formatFloatSpec(float64(n), sp)
	case float64:
		if sp.typ == 'd' || sp.typ == 'x' || sp.typ == 'c' || sp.typ == 's' {
			raise("ValueError", "Unknown format code '%c' for object of type 'float'", sp.typ)
		}
		return formatFloatSpec(x, sp)
	case nil, *Undefined, []any, Tuple, *Dict:
		if specText != "" {
			raise("TypeError", "unsupported format string passed to %s.__format__", typeName(v))
		}
		return Str(v)
	}
	return pad(Str(v), "", sp, '<')
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func formatFloatSpec(f float64, sp spec) string {
	var body string
	switch sp.typ {
	case 0:
		if !sp.hasPrec {
			body = pyfmtAbs(f)
		} else {
			p := sp.prec
			if p == 0 {
				p = 1
			}
			body = fmtFloat(f, 'g', p, sp.alt)
			if !strings.ContainsAny(body, ".einf") {
				body += ".0"
			}
		}
	case 'n':
		if !sp.hasPrec {
			sp.prec = 6
		}
		body = fmtFloat(f, 'g', sp.prec, sp.alt)
	default:
		if !sp.hasPrec {
			sp.prec = 6
		}
		body = fmtFloat(f, sp.typ, sp.prec, sp.alt)
	}
	if sp.grouping != 0 {
		body = groupDigits(body, sp.grouping)
	}
	neg := math.Signbit(f) && !math.IsNaN(f)
	return pad(body, signOf(neg, sp.sign), sp, '>')
}

// pyfmtAbs is Python's repr of the absolute value of a float.
func pyfmtAbs(f float64) string { return strings.TrimPrefix(Repr(math.Abs(f)), "-") }

// StrFormat is Python's str.format.
func StrFormat(format string, args []any, kwargs map[string]any) string {
	var b strings.Builder
	auto := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c == '}' {
			if i+1 < len(format) && format[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			raise("ValueError", "Single '}' encountered in format string")
		}
		if c != '{' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(format) && format[i+1] == '{' {
			b.WriteByte('{')
			i++
			continue
		}
		end := strings.IndexByte(format[i:], '}')
		if end < 0 {
			raise("ValueError", "Single '{' encountered in format string")
		}
		field := format[i+1 : i+end]
		i += end
		specText := ""
		if k := strings.IndexByte(field, ':'); k >= 0 {
			field, specText = field[:k], field[k+1:]
		}
		conv := byte(0)
		if k := strings.IndexByte(field, '!'); k >= 0 {
			if k+1 < len(field) {
				conv = field[k+1]
			}
			field = field[:k]
		}
		// The first part of the field name is a number or a name, then .attr and [key] parts.
		name := field
		rest := ""
		if k := strings.IndexAny(field, ".["); k >= 0 {
			name, rest = field[:k], field[k:]
		}
		var v any
		switch {
		case name == "":
			if auto >= len(args) {
				raise("IndexError", "Replacement index %d out of range for positional args tuple", auto)
			}
			v = args[auto]
			auto++
		case name[0] >= '0' && name[0] <= '9':
			n, _ := strconv.Atoi(name)
			if n >= len(args) {
				raise("IndexError", "Replacement index %d out of range for positional args tuple", n)
			}
			v = args[n]
		default:
			val, ok := kwargs[name]
			if !ok {
				raise("KeyError", "%s", reprString(name))
			}
			v = val
		}
		for rest != "" {
			if rest[0] == '.' {
				k := strings.IndexAny(rest[1:], ".[")
				attr := rest[1:]
				if k >= 0 {
					attr, rest = rest[1:1+k], rest[1+k:]
				} else {
					rest = ""
				}
				v = getAttr(v, attr)
			} else {
				k := strings.IndexByte(rest, ']')
				key := rest[1:k]
				rest = rest[k+1:]
				var kv any = key
				if n, err := strconv.ParseInt(key, 10, 64); err == nil {
					kv = n
				}
				v = getItem(v, kv)
			}
		}
		switch conv {
		case 'r', 'a':
			v = Repr(v)
		case 's':
			v = Str(v)
		}
		b.WriteString(FormatValue(v, specText))
	}
	return b.String()
}
