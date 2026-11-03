package api

import (
	"testing"

	"printdeck/internal/cloud"
)

// The pages put the shop link of a material in a link, so only an https address with a host
// name stays.
func TestShopURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.matterhackers.com/store/l/pla?sku=1": "https://www.matterhackers.com/store/l/pla?sku=1",
		" https://esun3d.com/pla ":                        "https://esun3d.com/pla",
		"http://shop.example/x":                           "",
		"javascript:alert(1)":                             "",
		"https://localhost/x":                             "",
		"https://user:pw@shop.example/x":                  "",
		"https://shop.example/a\nb":                       "",
		"//shop.example/x":                                "",
	} {
		if got := shopURL(in); got != want {
			t.Errorf("shopURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShopLinks(t *testing.T) {
	in := []cloud.MaterialLink{{Label: "2.85 mm", URL: "https://shop.example/a"}, {Label: "bad", URL: "http://shop.example/b"},
		{Label: "1.75 mm", URL: "https://shop.example/c"}, {Label: "a label that is far too long", URL: "https://shop.example/d"}}
	got := shopLinks(in)
	if len(got) != 2 || got[0].Label != "2.85 mm" || got[1].URL != "https://shop.example/c" {
		t.Errorf("shopLinks = %+v", got)
	}
}
