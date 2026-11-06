package printer

import (
	"testing"
)

func TestSetOptions(t *testing.T) {
	saveCfg := "#*# <---------------------- SAVE_CONFIG ---------------------->\n#*# [bed_mesh default]\n#*# x = 1\n"
	cases := []struct{ name, in, want string }{
		{"new section at the end", "[printer]\nkinematics: cartesian\n",
			"[printer]\nkinematics: cartesian\n\n[max_mode]\nenable: True\nmax_flow: 15\n"},
		{"new section before SAVE_CONFIG", "[printer]\nkinematics: cartesian\n\n" + saveCfg,
			"[printer]\nkinematics: cartesian\n\n[max_mode]\nenable: True\nmax_flow: 15\n\n" + saveCfg},
		{"options change in place", "[max_mode]\n# the hotend\nmax_flow: 12 # a test\nenable = False\n\n[printer]\nx: 1\n",
			"[max_mode]\n# the hotend\nmax_flow: 15\nenable: True\n\n[printer]\nx: 1\n"},
		{"a missing option goes after the last option", "[max_mode]\nenable: False\n# notes\n\n[printer]\n",
			"[max_mode]\nenable: True\nmax_flow: 15\n# notes\n\n[printer]\n"},
		{"a value on more lines", "[max_mode]\nmax_flow:\n  12\nenable: False\n",
			"[max_mode]\nmax_flow: 15\nenable: True\n"},
		{"other sections stay", "[max_mode_x]\nmax_flow: 1\n[max_mode]\nenable: False\n",
			"[max_mode_x]\nmax_flow: 1\n[max_mode]\nenable: True\nmax_flow: 15\n"},
	}
	for _, c := range cases {
		got := SetOptions(c.in, "max_mode", [][2]string{{"enable", "True"}, {"max_flow", "15"}})
		if got != c.want {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", c.name, got, c.want)
		}
		if err := CheckConfig(got); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
