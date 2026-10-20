// Package pyfmt reads and writes numbers as Python does: int(), float(), and str() of a float.
// Klipper's messages and command lines use these forms, so the Go port must give the same text.
package pyfmt

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var errSyntax = errors.New("invalid literal")

// stripUnderscores removes the underscores between digits, as Python allows them. An underscore
// at the start or end, or next to another one or a non-digit, is an error.
func stripUnderscores(s string) (string, error) {
	if !strings.Contains(s, "_") {
		return s, nil
	}
	isDigit := func(i int) bool { return i >= 0 && i < len(s) && s[i] >= '0' && s[i] <= '9' }
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if !isDigit(i-1) || !isDigit(i+1) {
				return "", errSyntax
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

// ParseInt reads an integer as Python's int() of a string.
func ParseInt(s string) (int64, error) {
	s, err := stripUnderscores(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(s, "+") {
		s = s[1:]
		if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
			return 0, errSyntax
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

// ParseFloat reads a number as Python's float() of a string.
func ParseFloat(s string) (float64, error) {
	s, err := stripUnderscores(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	body := strings.ToLower(strings.TrimLeft(s, "+-"))
	if len(s)-len(body) > 1 {
		return 0, errSyntax
	}
	switch body {
	case "inf", "infinity", "nan":
	default:
		if body == "" || strings.ContainsAny(body, "xpn_") || strings.HasPrefix(body, "i") {
			return 0, errSyntax
		}
	}
	return strconv.ParseFloat(s, 64)
}

// Float writes a float as Python's str() and repr(): the shortest form that reads back to the
// same value, with ".0" on whole numbers, and an exponent below 1e-4 and from 1e16.
func Float(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	exp := math.Floor(math.Log10(math.Abs(f)))
	// The shortest digits decide the exponent, as rounding can change it.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	if i := strings.IndexByte(e, 'e'); i >= 0 {
		n, _ := strconv.Atoi(e[i+1:])
		exp = float64(n)
	}
	if exp < -4 || exp >= 16 {
		mant, ex, _ := strings.Cut(e, "e")
		sign := ex[0]
		digits := strings.TrimLeft(ex[1:], "0")
		if len(digits) < 2 {
			digits = strings.Repeat("0", 2-len(digits)) + digits
		}
		return mant + "e" + string(sign) + digits
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
