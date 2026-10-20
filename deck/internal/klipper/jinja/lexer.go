package jinja

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// The lexer of the templates. The text between the tags is data. "{%" starts a block, "{" an
// expression, "{#" a comment. A "-" after the start or before the end of a tag removes the
// whitespace before or after the tag.

type tokType int

const (
	tData tokType = iota
	tBlockBegin
	tBlockEnd
	tVarBegin
	tVarEnd
	tName
	tString
	tInt
	tFloat
	tOp
	tEOF
)

type token struct {
	typ  tokType
	val  string
	line int
}

func (t token) String() string {
	switch t.typ {
	case tEOF:
		return "'end of template'"
	case tBlockEnd:
		return "'end of statement block'"
	case tVarEnd:
		return "'end of print statement'"
	case tData:
		return "'template data'"
	}
	return "'" + t.val + "'"
}

// SyntaxError is an error of the template text, with its line (TemplateSyntaxError).
type SyntaxError struct {
	Msg  string
	Line int
}

func (e *SyntaxError) Error() string { return e.Msg }

func syntaxErr(line int, format string, args ...any) {
	panic(&SyntaxError{fmt.Sprintf(format, args...), line})
}

var operators = []string{"//", "**", "==", "!=", ">=", "<=", "+", "-", "*", "/", "%", "~", "<", ">",
	"=", ".", ":", "|", ",", ";", "(", ")", "[", "]", "{", "}"}

// lex splits a template into tokens.
func lex(src string) []token {
	// Newlines are "\n", and one newline at the end of the template is dropped.
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	src = strings.TrimSuffix(src, "\n")
	var toks []token
	line := 1
	pos := 0
	lstrip := false // the last tag ended with "-": strip the whitespace at the start of the data
	for pos < len(src) {
		// Find the next tag.
		i := strings.IndexByte(src[pos:], '{')
		var data string
		if i < 0 {
			data = src[pos:]
		} else {
			data = src[pos : pos+i]
		}
		start := pos + len(data)
		kind := ""
		if i >= 0 && start+1 < len(src) && src[start+1] == '%' {
			kind = "block"
		} else if i >= 0 && start+1 < len(src) && src[start+1] == '#' {
			kind = "comment"
		} else if i >= 0 {
			kind = "var"
		}
		openLen := 2
		if kind == "var" {
			openLen = 1
		}
		rstrip := kind != "" && start+openLen < len(src) && src[start+openLen] == '-'
		if lstrip {
			data = strings.TrimLeftFunc(data, unicode.IsSpace)
		}
		if rstrip {
			data = strings.TrimRightFunc(data, unicode.IsSpace)
		}
		if data != "" {
			toks = append(toks, token{tData, data, line})
		}
		line += strings.Count(src[pos:start], "\n")
		if kind == "" {
			break
		}
		pos = start + openLen
		if rstrip {
			pos++
		}
		switch kind {
		case "comment":
			end := strings.Index(src[pos:], "#}")
			if end < 0 {
				syntaxErr(line, "Missing end of comment tag")
			}
			body := src[pos : pos+end]
			lstrip = strings.HasSuffix(body, "-")
			line += strings.Count(src[pos:pos+end+2], "\n")
			pos += end + 2
		case "block":
			toks = append(toks, token{tBlockBegin, "{%", line})
			var end int
			toks, pos, line, end = lexExpr(src, pos, line, toks, "%}")
			lstrip = end == 1
			toks = append(toks, token{tBlockEnd, "%}", line})
		case "var":
			toks = append(toks, token{tVarBegin, "{", line})
			var end int
			toks, pos, line, end = lexExpr(src, pos, line, toks, "}")
			lstrip = end == 1
			toks = append(toks, token{tVarEnd, "}", line})
		}
	}
	return append(toks, token{tEOF, "", line})
}

// lexExpr reads the tokens of an expression up to the end of the tag. It returns the tokens, the
// position after the tag, the line, and 1 when the end of the tag had a "-".
func lexExpr(src string, pos, line int, toks []token, closer string) ([]token, int, int, int) {
	var balance []byte
	for {
		// Skip whitespace
		for pos < len(src) && unicode.IsSpace(rune(src[pos])) {
			if src[pos] == '\n' {
				line++
			}
			pos++
		}
		if pos >= len(src) {
			syntaxErr(line, "unexpected end of template, expected '%s'", closer)
		}
		if len(balance) == 0 {
			if strings.HasPrefix(src[pos:], "-"+closer) {
				return toks, pos + 1 + len(closer), line, 1
			}
			if strings.HasPrefix(src[pos:], closer) {
				return toks, pos + len(closer), line, 0
			}
		}
		c := src[pos]
		switch {
		case c == '_' || unicode.IsLetter(rune(c)):
			end := pos
			for end < len(src) && (src[end] == '_' || unicode.IsLetter(rune(src[end])) || unicode.IsDigit(rune(src[end]))) {
				end++
			}
			toks = append(toks, token{tName, src[pos:end], line})
			pos = end
		case c >= '0' && c <= '9':
			end := pos
			isFloat := false
			for end < len(src) && (src[end] >= '0' && src[end] <= '9' || src[end] == '_') {
				end++
			}
			if end+1 < len(src) && src[end] == '.' && src[end+1] >= '0' && src[end+1] <= '9' {
				isFloat = true
				end++
				for end < len(src) && (src[end] >= '0' && src[end] <= '9' || src[end] == '_') {
					end++
				}
			}
			if end < len(src) && (src[end] == 'e' || src[end] == 'E') {
				e := end + 1
				if e < len(src) && (src[e] == '+' || src[e] == '-') {
					e++
				}
				if e < len(src) && src[e] >= '0' && src[e] <= '9' {
					isFloat = true
					end = e
					for end < len(src) && src[end] >= '0' && src[end] <= '9' {
						end++
					}
				}
			}
			typ := tInt
			if isFloat {
				typ = tFloat
			}
			toks = append(toks, token{typ, strings.ReplaceAll(src[pos:end], "_", ""), line})
			pos = end
		case c == '\'' || c == '"':
			s, end := lexString(src, pos, line)
			line += strings.Count(src[pos:end], "\n")
			toks = append(toks, token{tString, s, line})
			pos = end
		default:
			op := ""
			for _, o := range operators {
				if strings.HasPrefix(src[pos:], o) {
					op = o
					break
				}
			}
			if op == "" {
				syntaxErr(line, "unexpected char %s at %d", reprString(string(c)), pos)
			}
			switch op {
			case "(", "[", "{":
				balance = append(balance, op[0])
			case ")", "]", "}":
				want := map[string]byte{")": '(', "]": '[', "}": '{'}[op]
				if len(balance) == 0 || balance[len(balance)-1] != want {
					syntaxErr(line, "unexpected '%s'", op)
				}
				balance = balance[:len(balance)-1]
			}
			toks = append(toks, token{tOp, op, line})
			pos += len(op)
		}
	}
}

// lexString reads a string literal with the escapes of Python.
func lexString(src string, pos, line int) (string, int) {
	q := src[pos]
	var b strings.Builder
	i := pos + 1
	for i < len(src) {
		c := src[i]
		if c == q {
			return b.String(), i + 1
		}
		if c == '\\' && i+1 < len(src) {
			i++
			switch src[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\', '\'', '"':
				b.WriteByte(src[i])
			case '0':
				b.WriteByte(0)
			case 'x':
				if i+2 < len(src) {
					if n, err := strconv.ParseUint(src[i+1:i+3], 16, 8); err == nil {
						b.WriteRune(rune(n))
						i += 2
						break
					}
				}
				b.WriteString(`\x`)
			case 'u':
				if i+4 < len(src) {
					if n, err := strconv.ParseUint(src[i+1:i+5], 16, 32); err == nil {
						b.WriteRune(rune(n))
						i += 4
						break
					}
				}
				b.WriteString(`\u`)
			case '\n':
			default:
				b.WriteByte('\\')
				b.WriteByte(src[i])
			}
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	syntaxErr(line, "unexpected end of string")
	return "", 0
}
