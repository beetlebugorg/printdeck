// Package connect talks to go-librespot on this speaker, for the volume. When the volume slider
// of the Spotify app moves, go-librespot sends an event, and the amp follows.
package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/net/websocket"
)

type Client struct {
	addr string // host:port of the go-librespot API
	http *http.Client
}

func New(addr string) *Client {
	return &Client{addr: addr, http: &http.Client{Timeout: 3 * time.Second}}
}

// SetVolume tells Spotify the volume, so its app shows the volume of the amp. percent is 0..100.
func (c *Client) SetVolume(percent int) {
	b, _ := json.Marshal(map[string]int{"volume": percent})
	resp, err := c.http.Post("http://"+c.addr+"/player/volume", "application/json", bytes.NewReader(b))
	if err == nil {
		resp.Body.Close()
	}
}

// Run calls onVolume with a percent for each volume change in the Spotify app, and onConnect each time
// it connects. It reconnects when go-librespot restarts.
func (c *Client) Run(ctx context.Context, onVolume func(percent int), onConnect func()) {
	for ctx.Err() == nil {
		c.session(ctx, onVolume, onConnect)
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *Client) session(ctx context.Context, onVolume func(int), onConnect func()) {
	// go-librespot accepts a browser origin only from its own address.
	ws, err := websocket.Dial("ws://"+c.addr+"/events", "", "http://"+c.addr)
	if err != nil {
		return
	}
	defer ws.Close()
	go func() { <-ctx.Done(); ws.Close() }()
	onConnect()
	for {
		var m string
		if err := websocket.Message.Receive(ws, &m); err != nil {
			return
		}
		var ev struct {
			Type string
			Data struct{ Value, Max int }
		}
		if json.Unmarshal([]byte(m), &ev) == nil && ev.Type == "volume" && ev.Data.Max > 0 {
			onVolume(ev.Data.Value * 100 / ev.Data.Max)
		}
	}
}
