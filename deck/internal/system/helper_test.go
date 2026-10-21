package system

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startHelper(t *testing.T, allowed map[uint32]bool, idle time.Duration) chan error {
	t.Helper()
	HelperSocket = filepath.Join(t.TempDir(), "helper.sock")
	ln, err := net.Listen("unix", HelperSocket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serveHelper(ln.(*net.UnixListener), allowed, idle) }()
	return done
}

func TestHelper(t *testing.T) {
	var got []HelperRequest
	ran := false
	helperActions = func(r HelperRequest) (func(), error) {
		got = append(got, r)
		if r.Action == "restart-klipper" {
			return func() { ran = true }, nil
		}
		return nil, os.ErrInvalid
	}
	done := startHelper(t, map[uint32]bool{uint32(os.Getuid()): true}, 300*time.Millisecond)

	if err := AskHelper(HelperRequest{Action: "restart-klipper"}); err != nil {
		t.Fatal(err)
	}
	err := AskHelper(HelperRequest{Action: "wifi", SSID: "Workshop", Password: "ThisIsAPassword"})
	if err == nil || !strings.Contains(err.Error(), "invalid argument") {
		t.Errorf("the error of the action: %v", err)
	}
	if len(got) != 2 || got[1].SSID != "Workshop" || got[1].Password != "ThisIsAPassword" {
		t.Errorf("requests %+v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if !ran {
		t.Error("the function after the answer did not run")
	}
	// With no request, the helper stops.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the helper did not stop")
	}
}

func TestHelperRefusesOtherUsers(t *testing.T) {
	called := false
	helperActions = func(HelperRequest) (func(), error) { called = true; return nil, nil }
	startHelper(t, map[uint32]bool{12345: true}, time.Second)
	if err := AskHelper(HelperRequest{Action: "restart-klipper"}); err == nil || err.Error() != "not allowed" {
		t.Errorf("err = %v", err)
	}
	if called {
		t.Error("the action ran for a user that is not allowed")
	}
}
