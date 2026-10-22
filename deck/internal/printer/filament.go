package printer

import (
	"fmt"
	"math"
	"strings"
)

// The weight of the filament: a person buys filament by its weight, so the deck shows the
// filament of a print in grams. Klipper and most slicers count it in millimetres.

// densities are the densities of the materials of the deck, in g/cm³.
var densities = map[string]float64{"PLA": 1.24, "PETG": 1.27, "ABS": 1.04, "TPU": 1.21}

// Density is the density of a material of the deck in g/cm³. A material that the deck does not
// know counts as PLA.
func Density(material string) float64 {
	if rho, ok := densities[strings.ToUpper(strings.TrimSpace(material))]; ok {
		return rho
	}
	return densities["PLA"]
}

// FilamentGrams is the weight in grams of mm millimetres of filament with the diameter d in mm,
// of the material (see Density).
func FilamentGrams(mm, d float64, material string) float64 { return GramsAt(mm, d, Density(material)) }

// GramsAt is the weight in grams of mm millimetres of filament with the diameter d in mm and the
// density rho in g/cm³.
func GramsAt(mm, d, rho float64) float64 { return mm * math.Pi * d * d / 4 * rho / 1000 }

// Weight is a weight in grams for the screen: "4.2 g", "92 g" or "1.24 kg".
func Weight(g float64) string {
	switch {
	case g < 10:
		return fmt.Sprintf("%.1f g", g)
	case g < 1000:
		return fmt.Sprintf("%.0f g", g)
	}
	return fmt.Sprintf("%.2f kg", g/1000)
}

// Diameter is the filament diameter of the printer config, or 1.75 mm when the deck does not
// know it.
func (m Machine) Diameter() float64 {
	if m.FilamentDiameter > 0 {
		return m.FilamentDiameter
	}
	return 1.75
}
