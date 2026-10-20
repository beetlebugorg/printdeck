package pins

import "testing"

type chip struct{}

func (chip) SetupPin(pinType string, params *Params) any { return params }

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(*Error).Msg
		}
	}()
	f()
	return ""
}

func TestParsePin(t *testing.T) {
	p := New()
	p.RegisterChip("mcu", chip{})
	p.RegisterChip("probe", chip{})
	for desc, want := range map[string]Params{
		"PA1":                       {ChipName: "mcu", Pin: "PA1"},
		" ^! PC2 ":                  {ChipName: "mcu", Pin: "PC2", Pullup: 1, Invert: 1},
		"~probe: z_virtual_endstop": {ChipName: "probe", Pin: "z_virtual_endstop", Pullup: -1},
	} {
		got := p.ParsePin(desc, true, true)
		if got.ChipName != want.ChipName || got.Pin != want.Pin || got.Pullup != want.Pullup || got.Invert != want.Invert {
			t.Errorf("%q: %+v", desc, got)
		}
	}
	for desc, want := range map[string]string{
		"other:PA1": "Unknown pin chip name 'other'",
		"!PA1":      "Invalid pin description '!PA1'\nFormat is: [chip_name:] pin_name",
		"PA 1":      "Invalid pin description 'PA 1'\nFormat is: [chip_name:] pin_name",
	} {
		if msg := catch(func() { p.ParsePin(desc, false, false) }); msg != want {
			t.Errorf("%q: %q", desc, msg)
		}
	}
}

func TestLookupPin(t *testing.T) {
	p := New()
	p.RegisterChip("mcu", chip{})
	p.SetupPin("digital_out", "PA1")
	if msg := catch(func() { p.SetupPin("pwm", "PA1") }); msg != "pin PA1 used multiple times in config" {
		t.Errorf("%q", msg)
	}
	a := p.LookupPin("PB1", false, false, "spi")
	if b := p.LookupPin("PB1", false, false, "spi"); a != b {
		t.Error("shared pin has new params")
	}
	p.AllowMultiUsePin("PC1")
	p.SetupPin("endstop", "PC1")
	p.SetupPin("endstop", "^PC1")
}

func TestResolver(t *testing.T) {
	r := NewResolver(true)
	r.AliasPin("ar0", "PF0")
	r.AliasPin("analog0", "ar0")
	r.ReservePin("PA0", "usb")
	if got := r.UpdateCommand("config_analog_in oid=1 pin=ar0"); got != "config_analog_in oid=1 pin=PF0" {
		t.Errorf("%q", got)
	}
	if got := r.UpdateCommand("config_spi oid=2 cs_pin=PB4"); got != "config_spi oid=2 cs_pin=PB4" {
		t.Errorf("%q", got)
	}
	if msg := catch(func() { r.UpdateCommand("x pin=analog0") }); msg != "pin analog0 is an alias for ar0" {
		t.Errorf("%q", msg)
	}
	if msg := catch(func() { r.UpdateCommand("x pin=PA0") }); msg != "pin PA0 is reserved for usb" {
		t.Errorf("%q", msg)
	}
}
