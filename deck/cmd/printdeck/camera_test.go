package main

import (
	"testing"

	"printdeck/internal/camera"
)

// macOS and Windows have no capture, but they have the test camera when it is set.
func TestCameraOn(t *testing.T) {
	t.Setenv(camera.TestCameraEnv, "")
	if !cameraOn("linux") || cameraOn("darwin") || cameraOn("windows") {
		t.Error("with no test camera: want a camera only on Linux")
	}
	t.Setenv(camera.TestCameraEnv, "1")
	if !cameraOn("darwin") || !cameraOn("windows") {
		t.Error("with the test camera: want a camera on macOS and Windows")
	}
}
