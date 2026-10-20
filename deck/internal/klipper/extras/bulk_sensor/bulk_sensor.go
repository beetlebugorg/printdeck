// Package bulk_sensor collects data in batches and sends each batch to the API clients that
// subscribed to it.
//
// Ported to Go from klippy/extras/bulk_sensor.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. Only BatchBulkHelper and BatchWebhooksClient
// are ported: the classes for the sensor chips are not used by the ported modules.
//
//	Copyright (C) 2020-2023  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bulk_sensor

import (
	"log"
	"slices"

	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/webhooks"
)

// BatchInterval is the time between two batches.
const BatchInterval = 0.500

// ClientFunc gets a batch, and returns false when it needs no more batches.
type ClientFunc func(msg map[string]any) bool

type client struct {
	id int
	cb ClientFunc
}

// BatchBulkHelper calls the batch function periodically while there are clients.
type BatchBulkHelper struct {
	printer           *host.Printer
	batchCB           func(eventtime float64) map[string]any
	startCB, stopCB   func()
	isStarted         bool
	batchInterval     float64
	batchTimer        *reactor.Timer
	clientCBs         []client
	nextID            int
	webhooksStartResp map[string]any
}

// NewBatchBulkHelper makes a helper. startCB and stopCB can be nil.
func NewBatchBulkHelper(printer *host.Printer, batchCB func(eventtime float64) map[string]any,
	startCB, stopCB func(), batchInterval float64) *BatchBulkHelper {
	if startCB == nil {
		startCB = func() {}
	}
	if stopCB == nil {
		stopCB = func() {}
	}
	return &BatchBulkHelper{printer: printer, batchCB: batchCB, startCB: startCB, stopCB: stopCB,
		batchInterval: batchInterval, webhooksStartResp: map[string]any{}}
}

// callLogged runs f, and returns a command error of it.
func callLogged(msg string, f func()) (err *gcode.CommandError) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*gcode.CommandError)
			if !ok {
				panic(r)
			}
			log.Printf("%s: %v", msg, e)
			err = e
		}
	}()
	f()
	return nil
}

// Periodic batch processing

func (b *BatchBulkHelper) start() {
	if b.isStarted {
		return
	}
	b.isStarted = true
	if err := callLogged("BatchBulkHelper start callback error", b.startCB); err != nil {
		b.isStarted = false
		b.clientCBs = nil
		panic(err)
	}
	r := b.printer.Reactor()
	waketime := r.Monotonic() + b.batchInterval
	b.batchTimer = r.RegisterTimer(b.procBatch, waketime)
}

func (b *BatchBulkHelper) stop() {
	b.clientCBs = nil
	if b.batchTimer != nil {
		b.printer.Reactor().UnregisterTimer(b.batchTimer)
	}
	b.batchTimer = nil
	if !b.isStarted {
		return
	}
	if err := callLogged("BatchBulkHelper stop callback error", b.stopCB); err != nil {
		b.clientCBs = nil
	}
	b.isStarted = false
	if len(b.clientCBs) > 0 {
		// New client started while in process of stopping
		b.start()
	}
}

func (b *BatchBulkHelper) procBatch(eventtime float64) float64 {
	var msg map[string]any
	if err := callLogged("BatchBulkHelper batch callback error", func() { msg = b.batchCB(eventtime) }); err != nil {
		b.stop()
		return reactor.Never
	}
	if len(msg) == 0 {
		return eventtime + b.batchInterval
	}
	for _, c := range slices.Clone(b.clientCBs) {
		if !c.cb(msg) {
			// This client no longer needs updates - unregister it
			b.clientCBs = slices.DeleteFunc(b.clientCBs, func(o client) bool { return o.id == c.id })
			if len(b.clientCBs) == 0 {
				b.stop()
				return reactor.Never
			}
		}
	}
	return eventtime + b.batchInterval
}

// Client registration

// AddClient adds a client, and starts the batches.
func (b *BatchBulkHelper) AddClient(cb ClientFunc) {
	b.nextID++
	b.clientCBs = append(b.clientCBs, client{b.nextID, cb})
	b.start()
}

// Webhooks registration

func (b *BatchBulkHelper) addAPIClient(req *webhooks.WebRequest) {
	whbatch := newBatchWebhooksClient(req)
	b.AddClient(whbatch.handleBatch)
	req.Send(b.webhooksStartResp)
}

// AddMuxEndpoint adds an API endpoint that subscribes a client to the batches.
func (b *BatchBulkHelper) AddMuxEndpoint(path, key, value string, webhooksStartResp map[string]any) {
	b.webhooksStartResp = webhooksStartResp
	webhooks.Of(b.printer).RegisterMuxEndpoint(path, key, &value, b.addAPIClient)
}

// batchWebhooksClient sends the batches to an API client.
type batchWebhooksClient struct {
	cconn    webhooks.ClientConn
	template map[string]any
}

func newBatchWebhooksClient(req *webhooks.WebRequest) *batchWebhooksClient {
	return &batchWebhooksClient{cconn: req.ClientConnection(),
		template: req.GetDict("response_template", map[string]any{})}
}

func (c *batchWebhooksClient) handleBatch(msg map[string]any) bool {
	if c.cconn == nil || c.cconn.IsClosed() {
		return false
	}
	tmp := map[string]any{}
	for k, v := range c.template {
		tmp[k] = v
	}
	tmp["params"] = msg
	c.cconn.Send(tmp)
	return true
}
