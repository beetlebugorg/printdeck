package bambu

import (
	"archive/zip"
	"bytes"
	"testing"
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
