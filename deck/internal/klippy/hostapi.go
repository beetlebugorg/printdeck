package klippy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"printdeck/internal/hostapi"
)

// HostAPI returns the interface version (hostapi.Version) of the Klipper host on the socket at
// path, from its answer to "info". A host with no version, or no answer, is an error.
func HostAPI(ctx context.Context, path string) (int, error) {
	c, err := Dial(ctx, path)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	res, err := c.Call(ctx, "info", map[string]any{})
	if err != nil {
		return 0, err
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal(res, &info); err != nil {
		return 0, err
	}
	raw, ok := info[hostapi.InfoKey]
	if !ok {
		return 0, errors.New("the Klipper host has no interface version")
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("the interface version of the Klipper host: %w", err)
	}
	return v, nil
}
