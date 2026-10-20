module printdeck

go 1.25

require (
	golang.org/x/image v0.30.0
	golang.org/x/net v0.48.0
	rsc.io/qr v0.2.0
)

require github.com/jfreymuth/vorbis v1.0.2 // indirect

require (
	github.com/devgianlu/go-librespot v0.10.5
	golang.org/x/text v0.32.0 // indirect
)

replace github.com/devgianlu/go-librespot => ./third_party/go-librespot
