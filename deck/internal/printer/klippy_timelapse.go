package printer

import (
	"fmt"

	"printdeck/internal/timelapse"
)

// The time lapse of the Klipper host: the module printdeck_timelapse parks the toolhead at a layer
// change and waits for the picture of the deck (timelapse.Host).

// TimelapseStatus returns the status of the module printdeck_timelapse, and false with no
// connection to Klipper.
func (k *Klippy) TimelapseStatus() (timelapse.HostStatus, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.conn == nil || k.status == nil {
		return timelapse.HostStatus{}, false
	}
	s, ok := k.status["printdeck_timelapse"]
	if !ok {
		return timelapse.HostStatus{}, true
	}
	b := func(key string) bool { v, _ := s[key].(bool); return v }
	i := func(key string) int { return int(num(s[key])) }
	return timelapse.HostStatus{Present: true, Enable: b("enable"), Park: b("park"), First: i("first_layer"),
		Every: i("every"), Frame: i("frame"), Taken: i("taken"), Waiting: b("waiting"), IsFirst: b("first"),
		IsLapse: b("lapse"), Layer: i("layer"), Stopped: b("stopped")}, true
}

// TimelapseTaken sends the number n to the module: the picture of frame n is ready, and the print
// continues.
func (k *Klippy) TimelapseTaken(n int) error {
	return k.call("printdeck/timelapse/taken", map[string]any{"frame": n})
}

// SetTimelapse sends the settings of the time lapse to the module.
func (k *Klippy) SetTimelapse(enable, park bool, first, every int) error {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	return k.script(fmt.Sprintf("PRINTDECK_TIMELAPSE ENABLE=%d PARK=%d FIRST_LAYER=%d EVERY=%d", b(enable), b(park), first, every))
}
