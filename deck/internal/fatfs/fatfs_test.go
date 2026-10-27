package fatfs

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// memDevice is an image in memory.
type memDevice []byte

func (m memDevice) ReadBlock(lba uint32, b []byte) error {
	copy(b, m[int(lba)*BlockSize:])
	return nil
}

func testFiles() []BuildFile {
	t := time.Date(2026, 10, 27, 9, 30, 12, 0, time.Local)
	big := bytes.Repeat([]byte("G1 X10 Y10 E0.5 ; a line of G-code\n"), 4000) // over many clusters
	return []BuildFile{
		{Path: "BENCHY.GCO", Data: []byte("G28\n"), Modified: t},
		{Path: "Printdeck plaque 0.25mm PETG.gcode", Data: big, Modified: t},
		{Path: "parts/hex-planter_v3.gcode", Data: []byte("G28\nG1 Z5\n"), Modified: t},
		{Path: "parts/old/clip.gcode", Data: []byte(""), Modified: t},
	}
}

// Build makes a card that Open reads back: the folders, the long names, the sizes, the times and
// the content of each file, on FAT16 and on FAT32.
func TestBuildAndRead(t *testing.T) {
	for _, fat32 := range []bool{false, true} {
		img, err := Build(testFiles(), fat32)
		if err != nil {
			t.Fatal(err)
		}
		fs, err := Open(memDevice(img))
		if err != nil {
			t.Fatal(err)
		}
		if fs.fat32 != fat32 {
			t.Errorf("fat32 %v, want %v", fs.fat32, fat32)
		}
		root, err := fs.ReadDir("/")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range root {
			names = append(names, e.Name)
		}
		if got := strings.Join(names, "|"); got != "BENCHY.GCO|Printdeck plaque 0.25mm PETG.gcode|parts" {
			t.Errorf("fat32 %v: root %q", fat32, got)
		}
		if !root[2].Dir || root[0].Modified.Minute() != 30 {
			t.Errorf("fat32 %v: entries %+v", fat32, root)
		}
		sub, err := fs.ReadDir("PARTS")
		if err != nil || len(sub) != 2 || sub[0].Name != "hex-planter_v3.gcode" || !sub[1].Dir {
			t.Errorf("fat32 %v: parts %+v %v", fat32, sub, err)
		}
		for _, f := range testFiles() {
			r, err := fs.Open(f.Path)
			if err != nil {
				t.Fatalf("fat32 %v: %s: %v", fat32, f.Path, err)
			}
			got, err := io.ReadAll(r)
			if err != nil || !bytes.Equal(got, f.Data) {
				t.Errorf("fat32 %v: %s: %d bytes, want %d (%v)", fat32, f.Path, len(got), len(f.Data), err)
			}
		}
		if _, err := fs.Open("nothere.gcode"); err != ErrNotFound {
			t.Errorf("fat32 %v: a missing file: %v", fat32, err)
		}
	}
}

// A card with no partition table has the volume at block 0.
func TestNoPartitionTable(t *testing.T) {
	img, err := Build(testFiles(), false)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Open(memDevice(img[partStart*BlockSize:]))
	if err != nil {
		t.Fatal(err)
	}
	if list, err := fs.ReadDir(""); err != nil || len(list) != 3 {
		t.Errorf("root %+v %v", list, err)
	}
}
