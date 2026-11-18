package system

import "testing"

// The unit of a printer: the first printer is klipper.service. An ID that could leave the unit
// name or the folder fails.
func TestKlipperUnit(t *testing.T) {
	for id, want := range map[string]string{"": "klipper.service", "mini": "klipper@mini.service", "taz-2": "klipper@taz-2.service"} {
		if got, err := KlipperUnit(id); err != nil || got != want {
			t.Errorf("%q: %q %v", id, got, err)
		}
	}
	for _, id := range []string{"../etc", "a b", "Mini", "x.service", "-a", "a/b", "config", "abcdefghijklmnopq", "2mini", "a;reboot"} {
		if _, err := KlipperUnit(id); err == nil {
			t.Errorf("%q is valid", id)
		}
	}
}
