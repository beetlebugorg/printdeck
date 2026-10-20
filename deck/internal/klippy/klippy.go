// Package klippy is a client for the API socket of the Klipper host (klippy.py -a PATH).
//
// The protocol is from klippy/webhooks.py of Klipper (Copyright (C) 2020 Eric Callahan, GNU GPLv3).
// Each message is one JSON object followed by the byte 0x03:
//
//	request   {"id": 1, "method": "objects/query", "params": {...}}
//	answer    {"id": 1, "result": {...}}  or  {"id": 1, "error": {"error": "...", "message": "..."}}
//	update    {<the response_template of a subscription>, "params": {"eventtime": 1.5, "status": {...}}}
//
// A subscription answers with the full status, and then sends only the fields that changed.
//
// This package goes away when Printdeck has its own Go port of the Klipper host.
package klippy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
)

const separator = 0x03

// Error is an error answer from klippy, for example "Must home axis first".
type Error struct {
	Kind    string `json:"error"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

// ErrClosed is the error of a call on a closed connection.
var ErrClosed = errors.New("klippy: connection closed")

// Status is the state of printer objects: object name, then field name, then value.
// For example status["extruder"]["temperature"] is 209.6.
type Status map[string]map[string]any

// Merge puts the fields of an update into s.
func (s Status) Merge(update Status) {
	for name, fields := range update {
		obj := s[name]
		if obj == nil {
			obj = map[string]any{}
			s[name] = obj
		}
		for k, v := range fields {
			obj[k] = v
		}
	}
}

// Conn is one connection to klippy. It is safe for use by many goroutines.
type Conn struct {
	nc   net.Conn
	done chan struct{} // closed when the connection ends

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]chan message
	subs    map[string]func(eventtime float64, update Status)
	err     error // why the connection ended
}

type message struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
	Key    string          `json:"key"` // the response_template of the subscriptions of this package
	Params *struct {
		EventTime float64 `json:"eventtime"`
		Status    Status  `json:"status"`
	} `json:"params"`
}

// Dial connects to the klippy API socket at path.
func Dial(ctx context.Context, path string) (*Conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	c := &Conn{nc: nc, done: make(chan struct{}), pending: map[int]chan message{},
		subs: map[string]func(float64, Status){}}
	go c.read()
	return c, nil
}

// Done is closed when the connection ends, for example when klippy restarts.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err tells why the connection ended. It is nil while the connection is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection.
func (c *Conn) Close() error {
	err := c.nc.Close()
	<-c.done
	return err
}

// Call sends a request and waits for its answer. params can be nil.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.send(req); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-c.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Subscribe asks klippy for the fields of the objects, for example {"extruder": {"temperature"}}.
// A nil field list means all fields. It returns the full status now, and then calls update with
// each change until the connection ends. update runs on the reading goroutine: keep it short.
func (c *Conn) Subscribe(ctx context.Context, objects map[string][]string, update func(eventtime float64, update Status)) (Status, error) {
	c.mu.Lock()
	key := "printdeck-" + strconv.Itoa(len(c.subs)+1)
	c.subs[key] = update
	c.mu.Unlock()
	res, err := c.Call(ctx, "objects/subscribe", map[string]any{
		"objects":           objects,
		"response_template": map[string]any{"key": key},
	})
	if err != nil {
		c.mu.Lock()
		delete(c.subs, key)
		c.mu.Unlock()
		return nil, err
	}
	var first struct {
		Status Status `json:"status"`
	}
	if err := json.Unmarshal(res, &first); err != nil {
		return nil, fmt.Errorf("klippy: subscribe answer: %w", err)
	}
	return first.Status, nil
}

// Query reads the fields of the objects once. A nil field list means all fields.
func (c *Conn) Query(ctx context.Context, objects map[string][]string) (Status, error) {
	res, err := c.Call(ctx, "objects/query", map[string]any{"objects": objects})
	if err != nil {
		return nil, err
	}
	var q struct {
		Status Status `json:"status"`
	}
	if err := json.Unmarshal(res, &q); err != nil {
		return nil, fmt.Errorf("klippy: query answer: %w", err)
	}
	return q.Status, nil
}

// Script runs G-code, for example "G28" or "SDCARD_PRINT_FILE FILENAME=part.gcode". It returns when
// the G-code is done, so a long move or a heat-up keeps it waiting.
func (c *Conn) Script(ctx context.Context, gcode string) error {
	_, err := c.Call(ctx, "gcode/script", map[string]any{"script": gcode})
	return err
}

func (c *Conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.nc.Write(append(b, separator)); err != nil {
		return ErrClosed
	}
	return nil
}

// read gives each answer to its call and each update to its subscription, until the connection ends.
func (c *Conn) read() {
	r := bufio.NewReader(c.nc)
	var err error
	for {
		var b []byte
		if b, err = r.ReadBytes(separator); err != nil {
			break
		}
		var m message
		if json.Unmarshal(b[:len(b)-1], &m) != nil {
			continue // klippy logs bad messages too, and goes on
		}
		c.mu.Lock()
		if m.ID != nil {
			if ch := c.pending[*m.ID]; ch != nil {
				ch <- m
			}
			c.mu.Unlock()
			continue
		}
		update := c.subs[m.Key]
		c.mu.Unlock()
		if update != nil && m.Params != nil {
			update(m.Params.EventTime, m.Params.Status)
		}
	}
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	c.nc.Close()
	close(c.done)
}
