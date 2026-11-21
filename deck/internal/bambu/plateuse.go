package bambu

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PlateUse is the filament that the G-code of a plate pushes through the nozzle, by filament, at
// each mark of its progress. The count includes each move with E: the purge lines, the flush of a
// filament change and the prime tower. Bambu Studio counts the same moves for used_m of
// slice_info.config.
type PlateUse struct {
	// Marks are the points of the file with "M73 L" (the layer that the printer reports) or
	// "M73 P" (its percent), in the order of the file.
	Marks []UseMark
	// Total is the length of each filament in mm for all the file, by filament number less 1.
	Total []float64
}

// UseMark is the filament that the file used before a mark.
type UseMark struct {
	Layer   int       // the last "M73 L" before the mark, or 0
	Percent int       // the last "M73 P" before the mark, or 0
	MM      []float64 // the length of each filament in mm, by filament number less 1
}

// maxFilaments is the highest number of filaments that PlateUse counts. A higher T (T65535,
// T65279) is a command of the printer and CountUse ignores it.
const maxFilaments = 64

// PlateUseOf reads the G-code of the plate (its index, from 1) of the sliced 3MF in r, and counts
// its filament. With plate 0, it reads plate 1.
func PlateUseOf(r io.ReaderAt, size int64, plate int) (PlateUse, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return PlateUse{}, err
	}
	f, err := zr.Open(Plate{Index: max(1, plate)}.GcodePath())
	if err != nil {
		return PlateUse{}, fmt.Errorf("no G-code of plate %d: %w", max(1, plate), err)
	}
	defer f.Close()
	return CountUse(f)
}

// CountUse counts the filament of the G-code in r.
//
// A move with E before the first T goes to the first filament that the file loads: the start of
// the file purges the filament that the print starts with.
func CountUse(r io.Reader) (PlateUse, error) {
	var (
		u        PlateUse
		mm       []float64
		early    float64 // the E before the first T
		tool     = -1
		relative = true // Bambu Studio writes M83
		lastE    float64
		layer    int
		percent  = -1 // no M73 P yet
	)
	grow := func(n int) {
		for len(mm) <= n {
			mm = append(mm, 0)
		}
	}
	mark := func() {
		u.Marks = append(u.Marks, UseMark{Layer: layer, Percent: percent, MM: append([]float64(nil), mm...)})
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		w := strings.Fields(line)
		if len(w) == 0 {
			continue
		}
		switch c := w[0]; {
		case c == "M83":
			relative = true
		case c == "M82":
			relative = false
		case c == "G92":
			if v, ok := word(w, 'E'); ok {
				lastE = v
			}
		case c == "M73":
			if v, ok := word(w, 'L'); ok {
				layer = int(v)
				mark()
			}
			if v, ok := word(w, 'P'); ok && int(v) != percent {
				percent = int(v)
				mark()
			}
		case len(c) > 1 && c[0] == 'T':
			n, err := strconv.Atoi(c[1:])
			if err != nil || n < 0 || n >= maxFilaments {
				continue
			}
			if tool < 0 {
				grow(n)
				mm[n] += early
				early = 0
			}
			tool = n
			grow(n)
		case c == "G0" || c == "G1" || c == "G2" || c == "G3":
			v, ok := word(w, 'E')
			if !ok {
				continue
			}
			d := v
			if !relative {
				d, lastE = v-lastE, v
			}
			if tool < 0 {
				early += d
			} else {
				mm[tool] += d
			}
		}
	}
	if err := sc.Err(); err != nil {
		return PlateUse{}, err
	}
	u.Total = mm
	return u, nil
}

// word returns the value of the word with the letter l in the command w ("E1.5" gives 1.5).
func word(w []string, l byte) (float64, bool) {
	for _, s := range w[1:] {
		if len(s) > 1 && s[0] == l {
			v, err := strconv.ParseFloat(s[1:], 64)
			return v, err == nil
		}
	}
	return 0, false
}

// At returns the part of each filament, 0 to 1, that the print used at the layer and the percent
// that the printer reports: the last mark at or before both. The result is by filament number
// less 1.
func (u PlateUse) At(layer, percent int) []float64 {
	var at []float64
	for _, m := range u.Marks {
		if m.Layer > layer || m.Percent > percent {
			break
		}
		at = m.MM
	}
	out := make([]float64, len(u.Total))
	for i, t := range u.Total {
		if i < len(at) && t > 0 {
			out[i] = max(0, min(1, at[i]/t))
		}
	}
	return out
}
