package api

import "testing"

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
