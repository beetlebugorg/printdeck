package bambu

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"strconv"
)

// Filament is one filament of a plate of a sliced 3MF of Bambu Studio or OrcaSlicer, from
// Metadata/slice_info.config.
type Filament struct {
	ID     int    // the filament number of the slicer, from 1
	Type   string // "PLA"
	Color  string // "#RRGGBB"
	Grams  float64
	Meters float64
}

// sliceInfo is Metadata/slice_info.config.
type sliceInfo struct {
	Plates []struct {
		Meta []struct {
			Key   string `xml:"key,attr"`
			Value string `xml:"value,attr"`
		} `xml:"metadata"`
		Filaments []struct {
			ID    string `xml:"id,attr"`
			Type  string `xml:"type,attr"`
			Color string `xml:"color,attr"`
			UsedG string `xml:"used_g,attr"`
			UsedM string `xml:"used_m,attr"`
		} `xml:"filament"`
	} `xml:"plate"`
}

// ErrNoSliceInfo is a 3MF with no sliced plate.
var ErrNoSliceInfo = errors.New("the file has no sliced plate")

// PlateFilaments returns the filaments of the plate (its index, from 1) of the sliced 3MF in r.
// With plate 0 or a plate that the file does not have, a file with one plate gives that plate.
func PlateFilaments(r io.ReaderAt, size int64, plate int) ([]Filament, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	f, err := zr.Open("Metadata/slice_info.config")
	if err != nil {
		return nil, ErrNoSliceInfo
	}
	defer f.Close()
	var si sliceInfo
	if err := xml.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&si); err != nil {
		return nil, err
	}
	pick := -1
	for i, p := range si.Plates {
		for _, m := range p.Meta {
			if m.Key == "index" && m.Value == strconv.Itoa(plate) {
				pick = i
			}
		}
	}
	if pick < 0 && len(si.Plates) == 1 {
		pick = 0
	}
	if pick < 0 {
		return nil, ErrNoSliceInfo
	}
	var out []Filament
	for _, fl := range si.Plates[pick].Filaments {
		id, _ := strconv.Atoi(fl.ID)
		g, _ := strconv.ParseFloat(fl.UsedG, 64)
		m, _ := strconv.ParseFloat(fl.UsedM, 64)
		out = append(out, Filament{ID: id, Type: fl.Type, Color: fl.Color, Grams: g, Meters: m})
	}
	return out, nil
}

// Plate is one sliced plate of a 3MF.
type Plate struct {
	Index     int     // from 1: the G-code of the plate is Metadata/plate_<Index>.gcode
	Seconds   float64 // the print time that the slicer predicts
	Grams     float64 // the filament of the plate
	Filaments []Filament
}

// GcodePath is the path of the G-code of the plate in the 3MF, for the start command.
func (p Plate) GcodePath() string { return fmt.Sprintf("Metadata/plate_%d.gcode", p.Index) }

// Plates returns the sliced plates of the 3MF in r, in their order.
func Plates(r io.ReaderAt, size int64) ([]Plate, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	f, err := zr.Open("Metadata/slice_info.config")
	if err != nil {
		return nil, ErrNoSliceInfo
	}
	defer f.Close()
	var si sliceInfo
	if err := xml.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&si); err != nil {
		return nil, err
	}
	var out []Plate
	for _, sp := range si.Plates {
		var p Plate
		for _, m := range sp.Meta {
			switch m.Key {
			case "index":
				p.Index, _ = strconv.Atoi(m.Value)
			case "prediction":
				p.Seconds, _ = strconv.ParseFloat(m.Value, 64)
			case "weight":
				p.Grams, _ = strconv.ParseFloat(m.Value, 64)
			}
		}
		for _, fl := range sp.Filaments {
			id, _ := strconv.Atoi(fl.ID)
			g, _ := strconv.ParseFloat(fl.UsedG, 64)
			m, _ := strconv.ParseFloat(fl.UsedM, 64)
			p.Filaments = append(p.Filaments, Filament{ID: id, Type: fl.Type, Color: fl.Color, Grams: g, Meters: m})
		}
		if p.Index > 0 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoSliceInfo
	}
	return out, nil
}

// PlatePicture returns the picture of the plate index of the 3MF in r (Metadata/plate_<n>.png),
// or nil.
func PlatePicture(r io.ReaderAt, size int64, index int) image.Image {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil
	}
	f, err := zr.Open(fmt.Sprintf("Metadata/plate_%d.png", index))
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(io.LimitReader(f, 8<<20))
	if err != nil {
		return nil
	}
	return img
}
