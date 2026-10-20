package configfile

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// FileConfig is the part of Python's configparser.RawConfigParser that Klipper uses, with the
// settings of Klipper: strict=False, inline_comment_prefixes=(';', '#'), and empty lines in values.
// The rules follow RawConfigParser._read of Python 3.13:
//
//   - A "[name]" line starts a section. A section that is there already gets the new options too.
//   - An "option: value" or "option = value" line sets an option. The option is in lower case.
//   - A line with more indentation than the option line continues its value, without the
//     indentation. An empty line in a value stays in it, except at its end.
//   - A comment starts with ';' or '#' at the start of a line or after whitespace.
type FileConfig struct {
	sections []string
	options  map[string][]string            // option names of each section, in order
	values   map[string]map[string][]string // the lines of each value, before the join
	defaults map[string][]string
	defOrder []string
	joined   bool
}

var (
	sectRE = regexp.MustCompile(`^\[(.+)\]`)
	optRE  = regexp.MustCompile(`^(.*?)\s*([=:])\s*(.*)$`)
)

// NewFileConfig makes an empty config.
func NewFileConfig() *FileConfig {
	return &FileConfig{options: map[string][]string{}, values: map[string]map[string][]string{},
		defaults: map[string][]string{}}
}

// ParseError is a line that is not a section, an option, or a value.
type ParseError struct {
	Source string
	Lines  []string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("Source contains parsing errors: '%s'\n\t%s", e.Source, strings.Join(e.Lines, "\n\t"))
}

// Read adds the sections and options of data (RawConfigParser.read_file). Each call starts with
// no current section.
func (fc *FileConfig) Read(data, source string) error {
	var cursect map[string][]string
	var sectname, optname string
	indentLevel := 0
	var bad []string
	lines := strings.SplitAfter(data, "\n")
	for lineno, line := range lines {
		line = strings.TrimSuffix(line, "\n")
		commentStart := math.MaxInt
		// strip inline comments: in rounds, each round finds the next ';' and the next '#', and
		// the first round with a valid comment start ends the search.
		prefixes := map[string]int{";": -1, "#": -1}
		for commentStart == math.MaxInt && len(prefixes) > 0 {
			next := map[string]int{}
			for prefix, idx := range prefixes {
				n := strings.Index(line[idx+1:], prefix)
				if n < 0 {
					continue
				}
				idx += 1 + n
				next[prefix] = idx
				if idx == 0 || unicode.IsSpace(rune(line[idx-1])) {
					commentStart = min(commentStart, idx)
				}
			}
			prefixes = next
		}
		// strip full line comments
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			commentStart = 0
		}
		hasComment := commentStart != math.MaxInt
		value := line
		if hasComment {
			value = line[:commentStart]
		}
		value = strings.TrimSpace(value)
		if value == "" {
			// add empty line to the value, but only if there was no comment on the line
			if !hasComment && cursect != nil && optname != "" && cursect[optname] != nil {
				cursect[optname] = append(cursect[optname], "")
			}
			continue
		}
		// continuation line?
		curIndentLevel := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
		if cursect != nil && optname != "" && curIndentLevel > indentLevel {
			cursect[optname] = append(cursect[optname], value)
			continue
		}
		// a section header or option header?
		indentLevel = curIndentLevel
		if m := sectRE.FindStringSubmatch(value); m != nil {
			sectname = m[1]
			if sectname == "DEFAULT" {
				cursect = fc.defaults
			} else if s, ok := fc.values[sectname]; ok {
				cursect = s
			} else {
				cursect = map[string][]string{}
				fc.values[sectname] = cursect
				fc.sections = append(fc.sections, sectname)
			}
			optname = ""
			continue
		}
		if cursect == nil {
			return fmt.Errorf("File contains no section headers.\nfile: '%s', line: %d\n'%s'", source, lineno+1, line)
		}
		m := optRE.FindStringSubmatch(value)
		if m == nil || m[1] == "" {
			bad = append(bad, fmt.Sprintf("[line %2d]: '%s'", lineno+1, line))
			continue
		}
		optname = strings.ToLower(strings.TrimRight(m[1], " \t\r\f\v"))
		if _, ok := cursect[optname]; !ok {
			if sectname == "DEFAULT" {
				fc.defOrder = append(fc.defOrder, optname)
			} else {
				fc.options[sectname] = append(fc.options[sectname], optname)
			}
		}
		cursect[optname] = []string{strings.TrimSpace(m[3])}
	}
	fc.joined = false
	if len(bad) > 0 {
		return &ParseError{Source: source, Lines: bad}
	}
	return nil
}

// join joins the lines of a value and removes its trailing whitespace (_join_multiline_values).
func join(lines []string) string {
	return strings.TrimRightFunc(strings.Join(lines, "\n"), unicode.IsSpace)
}

// Sections lists the section names, in order.
func (fc *FileConfig) Sections() []string { return append([]string(nil), fc.sections...) }

// HasSection reports that a section is there.
func (fc *FileConfig) HasSection(section string) bool {
	_, ok := fc.values[section]
	return ok
}

// AddSection adds an empty section.
func (fc *FileConfig) AddSection(section string) {
	if !fc.HasSection(section) {
		fc.values[section] = map[string][]string{}
		fc.sections = append(fc.sections, section)
	}
}

// RemoveSection removes a section.
func (fc *FileConfig) RemoveSection(section string) {
	if !fc.HasSection(section) {
		return
	}
	delete(fc.values, section)
	delete(fc.options, section)
	for i, s := range fc.sections {
		if s == section {
			fc.sections = append(fc.sections[:i], fc.sections[i+1:]...)
			break
		}
	}
}

// Options lists the options of a section, with the DEFAULT options after them.
func (fc *FileConfig) Options(section string) []string {
	out := append([]string(nil), fc.options[section]...)
	for _, o := range fc.defOrder {
		if _, ok := fc.values[section][o]; !ok {
			out = append(out, o)
		}
	}
	return out
}

// HasOption reports that a section has an option. The option name is not case sensitive.
func (fc *FileConfig) HasOption(section, option string) bool {
	s, ok := fc.values[section]
	if !ok {
		return false
	}
	option = strings.ToLower(option)
	if _, ok := s[option]; ok {
		return true
	}
	_, ok = fc.defaults[option]
	return ok
}

// Get returns the value of an option, and false when it is not there.
func (fc *FileConfig) Get(section, option string) (string, bool) {
	s, ok := fc.values[section]
	if !ok {
		return "", false
	}
	option = strings.ToLower(option)
	if v, ok := s[option]; ok {
		return join(v), true
	}
	if v, ok := fc.defaults[option]; ok {
		return join(v), true
	}
	return "", false
}

// Set sets an option of a section that is there.
func (fc *FileConfig) Set(section, option, value string) {
	s := fc.values[section]
	option = strings.ToLower(option)
	if _, ok := s[option]; !ok {
		fc.options[section] = append(fc.options[section], option)
	}
	s[option] = []string{value}
}

// String writes the config in the form of RawConfigParser.write.
func (fc *FileConfig) String() string {
	var b strings.Builder
	if len(fc.defOrder) > 0 {
		b.WriteString("[DEFAULT]\n")
		for _, o := range fc.defOrder {
			writeOption(&b, o, join(fc.defaults[o]))
		}
		b.WriteString("\n")
	}
	for _, s := range fc.sections {
		fmt.Fprintf(&b, "[%s]\n", s)
		for _, o := range fc.options[s] {
			writeOption(&b, o, join(fc.values[s][o]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeOption(b *strings.Builder, option, value string) {
	value = strings.ReplaceAll(value, "\n", "\n\t")
	fmt.Fprintf(b, "%s = %s\n", option, value)
}
