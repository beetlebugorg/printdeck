package klippy

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/hostapi"
)

func TestHostAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	with := newFake(t, func(req map[string]any) (any, string) {
		return map[string]any{"state": "ready", hostapi.InfoKey: 7}, ""
	})
	if v, err := HostAPI(ctx, with.path); err != nil || v != 7 {
		t.Errorf("HostAPI = %d, %v; want 7", v, err)
	}
	// An earlier host has no version: the deck must not switch.
	without := newFake(t, func(req map[string]any) (any, string) {
		return map[string]any{"state": "ready"}, ""
	})
	if _, err := HostAPI(ctx, without.path); err == nil {
		t.Error("a host with no version gave no error")
	}
	if _, err := HostAPI(ctx, t.TempDir()+"/none.sock"); err == nil {
		t.Error("a missing socket gave no error")
	}
}
