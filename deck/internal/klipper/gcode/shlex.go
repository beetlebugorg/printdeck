package gcode

import "errors"

// shlexSplit splits s as Python's shlex.shlex(s, posix=True) with whitespace_split=True and
// commenters="#;". Quotes group words, a backslash escapes the next character (in double quotes,
// only '"' and '\'), and a comment character ends the line.
func shlexSplit(s string) ([]string, error) {
	const (
		stWhite = iota
		stWord
		stSingle
		stDouble
	)
	var out []string
	var tok []rune
	quoted := false
	state := stWhite
	rs := []rune(s)
	emit := func() {
		if len(tok) > 0 || quoted {
			out = append(out, string(tok))
		}
		tok, quoted = nil, false
	}
	isSpace := func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch state {
		case stWhite, stWord:
			switch {
			case isSpace(r):
				if state == stWord {
					emit()
				}
				state = stWhite
			case r == '#' || r == ';':
				// The comment goes to the end of the line.
				for i < len(rs) && rs[i] != '\n' {
					i++
				}
				if state == stWord {
					emit()
				}
				state = stWhite
			case r == '\'':
				state, quoted = stSingle, true
			case r == '"':
				state, quoted = stDouble, true
			case r == '\\':
				if i+1 >= len(rs) {
					return nil, errors.New("No escaped character")
				}
				i++
				tok = append(tok, rs[i])
				state = stWord
			default:
				tok = append(tok, r)
				state = stWord
			}
		case stSingle:
			if r == '\'' {
				state = stWord
			} else {
				tok = append(tok, r)
			}
		case stDouble:
			switch {
			case r == '"':
				state = stWord
			case r == '\\':
				if i+1 >= len(rs) {
					return nil, errors.New("No escaped character")
				}
				if n := rs[i+1]; n == '"' || n == '\\' {
					i++
					tok = append(tok, n)
				} else {
					tok = append(tok, r)
				}
			default:
				tok = append(tok, r)
			}
		}
	}
	if state == stSingle || state == stDouble {
		return nil, errors.New("No closing quotation")
	}
	if state == stWord {
		emit()
	}
	return out, nil
}
