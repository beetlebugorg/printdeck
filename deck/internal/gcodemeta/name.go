package gcodemeta

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Name is the name of a print for the screens: the title of the file, or else the cleaned file
// name.
func (m Meta) Name(file string) string {
	if m.Title != "" {
		return m.Title
	}
	return CleanName(file)
}

var (
	// The machine prefix of Cura at the start of a job name: the abbreviation of the printer in
	// capitals and digits, for example "TAZWH_", "UMS3_" or "LTAZWSE05MM_".
	machinePrefix = regexp.MustCompile(`^([A-Z0-9]{2,12})_(.*)$`)
	// A version or a copy at the end: "_v3", "-v2", " v1", " (1)".
	versionTail = regexp.MustCompile(`(?:[_ -][vV]\d+|\s*\(\d+\))$`)
	spaces      = regexp.MustCompile(`\s+`)
)

// notMachine has the words in capitals that start a file name more often than a printer
// abbreviation does.
var notMachine = map[string]bool{
	"PLA": true, "PETG": true, "PET": true, "ABS": true, "ASA": true, "TPU": true, "PC": true, "PA": true,
	"PP": true, "PVA": true, "HIPS": true, "CF": true, "NYLON": true, "LED": true, "USB": true, "LCD": true,
	"PCB": true, "DIY": true, "RC": true,
}

// CleanName makes a name for the screens from a G-code file name:
//
//   - It removes the folder and the extension .gcode, .gco or .g.
//   - It removes the machine prefix of Cura ("TAZWH_hex_planter" is "hex_planter").
//   - It removes a version or a copy number at the end ("_v3", "-v2", " (1)").
//   - It changes "_" and "-" between words to spaces. A "-" between two digits stays.
//   - It makes the first letter a capital, and keeps the other letters.
//
// When the rules leave an empty name, the result is the file name without the extension.
func CleanName(file string) string {
	base := path.Base(strings.ReplaceAll(file, `\`, "/"))
	if file == "" || base == "." || base == "/" {
		return file
	}
	stem := base
	switch strings.ToLower(path.Ext(base)) {
	case ".gcode", ".gco", ".g":
		stem = strings.TrimSuffix(base, path.Ext(base))
	}
	s := stem
	if p := machinePrefix.FindStringSubmatch(s); p != nil && !notMachine[p[1]] && strings.ContainsFunc(p[1], unicode.IsLetter) &&
		strings.Trim(p[2], "_- ") != "" {
		s = p[2]
	}
	for {
		t := versionTail.ReplaceAllString(s, "")
		if t == s || strings.Trim(t, "_- ") == "" {
			break
		}
		s = t
	}
	s = spaceWords(s)
	if s == "" {
		if stem != "" {
			return stem
		}
		return base
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// spaceWords changes "_" and "-" to spaces, except a "-" between two digits, and removes the
// extra spaces.
func spaceWords(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		if r == '_' || (r == '-' && (i == 0 || i == len(rs)-1 || !unicode.IsDigit(rs[i-1]) || !unicode.IsDigit(rs[i+1]))) {
			rs[i] = ' '
		}
	}
	return strings.TrimSpace(spaces.ReplaceAllString(string(rs), " "))
}
