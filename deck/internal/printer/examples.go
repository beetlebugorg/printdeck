package printer

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// The example configs of Klipper for complete printers (config/printer-*.cfg), for the setup of a
// new printer. examples/update.sh makes printers.gz.
//
//go:embed examples/printers.gz
var examplesGz []byte

// Example is the example config of one printer.
type Example struct {
	File  string // for example "printer-lulzbot-taz6-2017.cfg"
	Brand string // "LulzBot"
	Model string // "TAZ6"
	Year  string // "2017"
	MCU   string // the chip that the firmware is for, for example "atmega2560", or ""
}

// Name is the brand and the model, for example "LulzBot TAZ6".
func (e Example) Name() string { return strings.TrimSpace(e.Brand + " " + e.Model) }

// Fits says if the example is for the chip of a board, for example "stm32f103xe".
func (e Example) Fits(mcu string) bool {
	return e.MCU != "" && mcu != "" && strings.HasPrefix(strings.ToLower(mcu), e.MCU)
}

var (
	examplesOnce sync.Once
	examples     []Example
	exampleText  map[string]string
)

func loadExamples() {
	exampleText = map[string]string{}
	r, err := gzip.NewReader(bytes.NewReader(examplesGz))
	if err != nil {
		return
	}
	b, _ := io.ReadAll(r)
	for _, part := range strings.Split(string(b), "\x1e")[1:] {
		name, text, _ := strings.Cut(part, "\n")
		exampleText[name] = text
		examples = append(examples, parseExample(name, text))
	}
}

// Examples returns the example configs, in the order of their file names.
func Examples() []Example {
	examplesOnce.Do(loadExamples)
	return examples
}

// brands are the names of the brands with capitals that a file name does not have.
var brands = map[string]string{
	"adimlab": "ADIMLab", "anet": "Anet", "anycubic": "Anycubic", "artillery": "Artillery", "biqu": "BIQU",
	"creality": "Creality", "elegoo": "Elegoo", "flashforge": "FlashForge", "flsun": "FLSUN",
	"geeetech": "Geeetech", "lulzbot": "LulzBot", "mbot": "MBot", "monoprice": "Monoprice",
	"prusa": "Prusa", "sovol": "Sovol", "tronxy": "Tronxy", "twotrees": "TwoTrees", "wanhao": "Wanhao",
	"tevo": "Tevo", "qidi": "QIDI", "voron": "Voron", "kingroon": "Kingroon", "ultimaker": "Ultimaker",
}

// modelWords are the words of the model names with capitals that a file name does not have.
var modelWords = map[string]string{"taz": "TAZ", "i3": "i3", "qqs": "QQS", "bx": "BX", "se": "SE", "ht": "HT", "sp": "SP"}

var (
	yearRe = regexp.MustCompile(`^(19|20)\d\d$`)
	mcuRe  = regexp.MustCompile(`(atmega\d+p?|at90usb\d+|stm32[a-z]\d+[a-z0-9]*|gd32f\d+|lpc17\d\d|sam[34][a-z0-9]+|samd\d+|rp2040|hc32f\d+|n32g\d+)`)
)

// parseExample reads the name of the printer from the file name, and the chip from the comments at
// the top of the file.
func parseExample(file, text string) Example {
	e := Example{File: file}
	words := strings.Split(strings.TrimSuffix(strings.TrimPrefix(file, "printer-"), ".cfg"), "-")
	if n := len(words); n > 1 && yearRe.MatchString(words[n-1]) {
		e.Year, words = words[n-1], words[:n-1]
	}
	if len(words) > 0 {
		e.Brand = brands[words[0]]
		if e.Brand == "" {
			e.Brand = strings.ToUpper(words[0][:1]) + words[0][1:]
		}
		for i, w := range words[1:] {
			if m, ok := modelWords[strings.ToLower(w)]; ok {
				w = m
			} else if strings.ContainsAny(w, "0123456789") {
				w = strings.ToUpper(w)
			} else {
				w = strings.ToUpper(w[:1]) + w[1:]
			}
			words[i+1] = w
		}
		e.Model = strings.Join(words[1:], " ")
	}
	var head strings.Builder
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if strings.HasPrefix(l, "[") {
			break
		}
		head.WriteString(strings.ToLower(l) + " ")
	}
	if m := mcuRe.FindString(head.String()); m != "" {
		e.MCU = m
	}
	return e
}

// ExampleConfig returns the text of an example config, with serial as the serial port of [mcu]
// and the include of the Printdeck macros at the end.
func ExampleConfig(file, serial string) (string, error) {
	examplesOnce.Do(loadExamples)
	text, ok := exampleText[file]
	if !ok {
		return "", fmt.Errorf("no example config %q", file)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Klipper's example config %s, chosen on the Printdeck.\n"+
		"# Check each setting against your printer before you print.\n\n", file)
	section, done := "", false
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		l := s.Text()
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			if section == "[mcu]" && !done && serial != "" {
				out.WriteString("serial: " + serial + "\n")
				done = true
			}
			section = strings.ToLower(t)
		}
		if section == "[mcu]" && !done && serial != "" && strings.HasPrefix(t, "serial:") {
			l, done = "serial: "+serial, true
		}
		out.WriteString(l + "\n")
	}
	if section == "[mcu]" && !done && serial != "" {
		out.WriteString("serial: " + serial + "\n")
	}
	out.WriteString("\n# Added by Printdeck: keep this line last.\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n")
	return out.String(), nil
}

// UseExample writes an example config as printer.cfg at path. The old file stays as
// printer.cfg.bak.
func UseExample(path, file, serial string) error {
	text, err := ExampleConfig(file, serial)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		os.WriteFile(path+".bak", old, 0o664)
	}
	tmp := filepath.Join(filepath.Dir(path), ".printer.cfg.new")
	if err := os.WriteFile(tmp, []byte(text), 0o664); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
