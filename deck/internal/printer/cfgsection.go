package printer

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/maxmode"
)

// SetOptions returns the text of a Klipper config file with the options of the section name set
// to the values. A line of an option that is in the section gets the new value; the other lines
// of the section stay. An option that is not in the section goes at the end of the section. A file
// with no such section gets the section at the end, before the SAVE_CONFIG block.
func SetOptions(text, name string, options [][2]string) string {
	lines := strings.Split(text, "\n")
	header := "[" + name + "]"
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if strings.EqualFold(t, header) && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
				start = i
			} else if strings.HasPrefix(t, "#*#") {
				end = i
				break
			}
			continue
		}
		if strings.HasPrefix(l, "[") || strings.HasPrefix(t, "#*#") {
			end = i
			break
		}
	}
	line := func(o [2]string) string { return o[0] + ": " + o[1] }
	if start < 0 {
		// A new section before the SAVE_CONFIG block, or at the end of the file.
		at := end
		for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		add := []string{"", header}
		for _, o := range options {
			add = append(add, line(o))
		}
		if at < len(lines) {
			add = append(add, "")
		}
		out := append(append(append([]string{}, lines[:at]...), add...), lines[end:]...)
		if at == len(lines) {
			out = append(out, "")
		}
		return strings.Join(out, "\n")
	}
	// The last line of the section that is not empty and not a comment: new options go after it.
	last := start
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if t != "" && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, ";") {
			last = i
		}
	}
	var missing []string
	for _, o := range options {
		found := false
		for i := start + 1; i < end; i++ {
			l := lines[i]
			if l == "" || l[0] == ' ' || l[0] == '\t' || l[0] == '#' || l[0] == ';' {
				continue
			}
			k, _, ok := strings.Cut(l, ":")
			if !ok {
				k, _, ok = strings.Cut(l, "=")
			}
			if ok && strings.EqualFold(strings.TrimSpace(k), o[0]) {
				lines[i] = line(o)
				found = true
				// A value on more than one line: the lines under it go.
				for i+1 < end && len(lines[i+1]) > 0 && (lines[i+1][0] == ' ' || lines[i+1][0] == '\t') {
					lines = append(lines[:i+1], lines[i+2:]...)
					end--
					if last > i {
						last--
					}
				}
				break
			}
		}
		if !found {
			missing = append(missing, line(o))
		}
	}
	out := append(append(append([]string{}, lines[:last+1]...), missing...), lines[last+1:]...)
	return strings.Join(out, "\n")
}

// CheckConfig returns an error for a config file that the config parser of Klipper cannot read.
func CheckConfig(text string) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the file cannot be read: %v", p)
		}
	}()
	return configfile.NewFileConfig().Read(text, "printer.cfg")
}

// MaxMode returns the settings of the [max_mode] section of the printer, and false for a printer
// with no section (the defaults, with the filament diameter of the extruder). Klipper must be
// ready.
func (k *Klippy) MaxMode() (maxmode.Settings, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.maxMode != nil {
		return *k.maxMode, true
	}
	s := maxmode.Defaults()
	if k.diameter > 0 {
		s.FilamentDiameter = k.diameter
	}
	return s, false
}

// SetMaxMode writes the settings to the [max_mode] section of printer.cfg, and restarts Klipper.
// It returns an error during a print.
func (k *Klippy) SetMaxMode(s maxmode.Settings) error {
	if k.ConfigPath == "" {
		return errors.New("The deck has no printer.cfg")
	}
	if k.State().Busy() {
		return errors.New("Wait until the print ends")
	}
	b, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	enable := "False"
	if s.Enable {
		enable = "True"
	}
	text := SetOptions(string(b), "max_mode", append([][2]string{{"enable", enable}}, s.Options()...))
	if err := CheckConfig(text); err != nil {
		return err
	}
	if err := os.WriteFile(k.ConfigPath, []byte(text), 0o664); err != nil {
		return err
	}
	k.configChanged()
	return k.RestartKlipper()
}
