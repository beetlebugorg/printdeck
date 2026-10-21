package ui

import (
	"bytes"
	"flag"
	"image/color"
	"os"
	"testing"
)

var updateLogo = flag.Bool("update-logo", false, "write the kernel logo of the OS image")

// logoFile is the start screen in the kernel of the OS image (os/board/linux.fragment).
const logoFile = "../../../os/board/firmware/printdeck-logo.tga"

// TestSplashLogo checks that the kernel logo is the start screen of this code. After a change to
// the start screen, run "go test ./internal/ui -run SplashLogo -update-logo".
func TestSplashLogo(t *testing.T) {
	got := SplashLogo(LogoRotate)
	if *updateLogo {
		if err := os.WriteFile(logoFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(logoFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the kernel logo is old: run go test ./internal/ui -run SplashLogo -update-logo")
	}
}

func TestSplashLogoFormat(t *testing.T) {
	b := SplashLogo(LogoRotate)
	w, h := int(b[12])|int(b[13])<<8, int(b[14])|int(b[15])<<8
	// The framebuffer is 800x480, and the kernel centers the logo on it.
	if w != logoRect.Dy() || h != logoRect.Dx() || len(b) != 18+w*h*3 {
		t.Fatalf("size %dx%d, %d bytes", w, h, len(b))
	}
	if (800-w)/2 != logoRect.Min.Y || (480-h)/2 != logoRect.Min.X {
		t.Fatal("the logo is not centered at the position of the plate")
	}
	colors := map[[3]byte]bool{}
	for i := 18; i < len(b); i += 3 {
		colors[[3]byte{b[i], b[i+1], b[i+2]}] = true
	}
	if len(colors) > logoColors {
		t.Fatalf("%d colors", len(colors))
	}
	// The kernel fills the rest of the screen with the first pixel. The logo covers the screen, so
	// the first pixel is a color of the empty display.
	px := color.RGBA{b[20], b[19], b[18], 255}
	if px != startBG && px != startCell {
		t.Fatalf("the first pixel %v is not a color of the display", px)
	}
}
