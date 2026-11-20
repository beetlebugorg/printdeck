package main

import (
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// bambuLapse is the time lapse of a Bambu Lab printer for the screens (ui.TimeLapse): the printer
// makes it. The toggle of Settings, Time lapse is the setting of the printer (ipcam_timelapse), and
// the videos are the copies of the deck (printer.Bambu.Videos).
type bambuLapse struct{ b *printer.Bambu }

func (l bambuLapse) Settings() timelapse.Settings {
	return timelapse.Settings{On: l.b.CameraChecks().Timelapse}
}

// SetSettings sends a change of the toggle to the printer. The other settings are for the time
// lapse of the deck.
func (l bambuLapse) SetSettings(s timelapse.Settings) {
	if s.On != l.b.CameraChecks().Timelapse {
		go l.b.SetCameraCheck(printer.CheckTimelapse, s.On)
	}
}

func (l bambuLapse) Info() timelapse.Info {
	st := l.b.State()
	return timelapse.Info{Recording: st.Busy() && l.b.CameraChecks().Timelapse, Videos: l.b.Videos()}
}

func (l bambuLapse) VideoPath(name string) (string, bool) { return l.b.VideoPath(name) }

// LapseNote tells where the videos of the printer are: the deck copies them from the card of the
// printer (a USB drive on the X2D).
func (l bambuLapse) LapseNote() string {
	if r, ok := l.b.Report(); ok && !r.SDCard {
		return "Put a USB drive in the printer: the deck copies each video from it"
	}
	return "The deck copies each video from the drive of the printer"
}
