package signing

import (
	"archive/zip"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// write3MF writes a 3MF with the entries name: content.
func write3MF(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".upload-123") // the temporary file of an upload has no extension
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range entries {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	z.Close()
	f.Close()
	return p
}

// Each plate of a 3MF needs a good signature: one plate that is not OK gives the verdict of the
// file. The other entries of the 3MF are not checked.
func TestCheck3MF(t *testing.T) {
	key := testKey(7)
	pub := key.Public().(ed25519.PublicKey)
	plate := sign(key, "G28\nG1 X10 Y10 F3000\n")
	for _, c := range []struct {
		name    string
		entries map[string]string
		want    Result
	}{
		{"one signed plate", map[string]string{"Metadata/plate_1.gcode": plate, "Metadata/slice_info.config": "<config/>"}, OK},
		{"zslice", map[string]string{"Metadata/plate_1.gcode": zsliceFile}, OK},
		{"two signed plates", map[string]string{"Metadata/plate_1.gcode": plate, "Metadata/plate_2.gcode": plate}, OK},
		{"an unsigned plate", map[string]string{"Metadata/plate_1.gcode": plate, "Metadata/plate_2.gcode": "G28\n"}, Unsigned},
		{"another key", map[string]string{"Metadata/plate_1.gcode": sign(testKey(9), "G28\n")}, UnknownKey},
		{"a changed plate", map[string]string{"Metadata/plate_1.gcode": "M104 S300\n" + plate}, Bad},
		{"no plate", map[string]string{"Metadata/slice_info.config": "<config/>"}, Unsigned},
	} {
		v, err := CheckFile(write3MF(t, c.entries), []ed25519.PublicKey{pub})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v.Result != c.want {
			t.Errorf("%s: %v, want %v", c.name, v.Result, c.want)
		}
	}
}
