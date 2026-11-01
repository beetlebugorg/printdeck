package api

import (
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

func TestStillSame(t *testing.T) {
	d1, d2 := strings.Repeat("a", 64), strings.Repeat("b", 64)
	old := &PlanRecord{Parts: []cloud.PlanPart{{Name: "Pod door", Variant: "fine", Digest: d1}, {Name: "Cover", Profile: "p@1"}}}
	door := []cloud.PlatePart{{Part: 0}}
	cover := []cloud.PlatePart{{Part: 1}}
	for _, c := range []struct {
		name  string
		parts []cloud.PlanPart
		plate []cloud.PlatePart
		same  bool
	}{
		{"the same digest", []cloud.PlanPart{{Name: "pod door", Variant: "Fine", Digest: d1, Changed: true}}, door, true},
		{"another digest", []cloud.PlanPart{{Name: "Pod door", Variant: "fine", Digest: d2}}, door, false},
		{"the part is gone", []cloud.PlanPart{{Name: "Pod door", Digest: d1}}, door, false},
		{"no digest, no change", []cloud.PlanPart{{Name: "Cover", Profile: "p@1"}}, cover, true},
		{"no digest, changed", []cloud.PlanPart{{Name: "Cover", Profile: "p@1", Changed: true}}, cover, false},
		{"no digest, another profile", []cloud.PlanPart{{Name: "Cover", Profile: "p@2"}}, cover, false},
		{"a bad index", []cloud.PlanPart{{Name: "Cover"}}, []cloud.PlatePart{{Part: 5}}, false},
	} {
		if got := stillSame(old, &PlanRecord{Parts: c.parts}, c.plate); got != c.same {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
