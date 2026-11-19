package bambu

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
	"time"
)

// test3MF makes a 3MF with the slice_info of two plates, as Bambu Studio writes it.
func test3MF(t *testing.T) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("Metadata/slice_info.config")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<config>
  <header><header_item key="X-BBL-Client-Type" value="slicer"/></header>
  <plate>
    <metadata key="index" value="1"/>
    <metadata key="prediction" value="2513"/>
    <filament id="1" tray_info_idx="GFL99" type="PLA" color="#898989" used_m="3.42" used_g="10.21"/>
  </plate>
  <plate>
    <metadata key="index" value="2"/>
    <filament id="1" tray_info_idx="GFL99" type="PLA" color="#898989" used_m="1.00" used_g="2.98"/>
    <filament id="4" tray_info_idx="GFL99" type="PLA" color="#F72323" used_m="0.50" used_g="1.49"/>
  </plate>
</config>`))
	z.Close()
	return b.Bytes()
}

func TestPlateFilaments(t *testing.T) {
	b := test3MF(t)
	fs, err := PlateFilaments(bytes.NewReader(b), int64(len(b)), 2)
	if err != nil || len(fs) != 2 || fs[1].ID != 4 || fs[1].Grams != 1.49 || fs[0].Color != "#898989" {
		t.Fatalf("%+v %v", fs, err)
	}
	if _, err := PlateFilaments(bytes.NewReader(b), int64(len(b)), 0); err != ErrNoSliceInfo {
		t.Errorf("two plates and no index: %v", err)
	}
}

func TestPlates(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("Metadata/slice_info.config")
	w.Write([]byte(`<config><plate><metadata key="index" value="2"/><metadata key="prediction" value="2513"/>` +
		`<metadata key="weight" value="12.50"/>` +
		`<filament id="1" type="PLA" color="#898989" used_m="3.4" used_g="10.21"/>` +
		`<filament id="3" type="PLA" color="#161616" used_m="0.8" used_g="2.29"/></plate></config>`))
	w, _ = z.Create("Metadata/plate_2.png")
	png.Encode(w, image.NewRGBA(image.Rect(0, 0, 4, 3)))
	z.Close()
	data := b.Bytes()
	ps, err := Plates(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(ps) != 1 {
		t.Fatalf("%+v %v", ps, err)
	}
	p := ps[0]
	if p.Index != 2 || p.Seconds != 2513 || p.Grams != 12.5 || len(p.Filaments) != 2 || p.GcodePath() != "Metadata/plate_2.gcode" {
		t.Errorf("plate %+v", p)
	}
	if img := PlatePicture(bytes.NewReader(data), int64(len(data)), 2); img == nil || img.Bounds().Dx() != 4 {
		t.Errorf("picture %v", img)
	}
}

func TestStartPrint(t *testing.T) {
	f, err := NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := &Client{Addr: f.Addr(), Serial: f.Serial, Code: f.Code}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go c.Run(ctx, nil)
	for c.StartPrint(Start{}) == ErrOffline {
		time.Sleep(10 * time.Millisecond)
	}
	err = c.StartPrint(Start{File: "cube.gcode.3mf", Name: "cube", Plate: "Metadata/plate_1.gcode", Trays: []int{0, -1, 5}, BedLevel: true})
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	for end := time.Now().Add(3 * time.Second); p == nil; {
		for _, r := range f.Requests() {
			if q, ok := r["print"].(map[string]any); ok && q["command"] == "project_file" && q["file"] == "cube.gcode.3mf" {
				p = q
			}
		}
		if time.Now().After(end) {
			t.Fatal("no project_file")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p["param"] != "Metadata/plate_1.gcode" || p["url"] != "ftp:///cube.gcode.3mf" || p["use_ams"] != true || p["bed_levelling"] != true {
		t.Errorf("start %v", p)
	}
	m := p["ams_mapping"].([]any)
	m2 := p["ams_mapping2"].([]any)
	if len(m) != 3 || m[0] != 0.0 || m[1] != -1.0 || m[2] != 5.0 {
		t.Errorf("mapping %v", m)
	}
	if third := m2[2].(map[string]any); third["ams_id"] != 1.0 || third["slot_id"] != 1.0 {
		t.Errorf("mapping2 %v", m2)
	}
}
