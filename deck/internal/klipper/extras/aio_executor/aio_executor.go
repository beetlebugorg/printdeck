// Package aio_executor runs blocking work, for example file reads, in a worker goroutine. The
// caller waits on a reactor completion, so the other reactor work continues.
//
// Ported to Go from klippy/extras/aio_executor.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2026  Timofey Titovets <nefelim4ag@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package aio_executor

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

type result struct {
	res any
	err error
}

type work struct {
	completion *reactor.Completion
	fn         func() (any, error)
}

// Executor is a worker goroutine.
type Executor struct {
	reactor     *reactor.Reactor
	queue       chan *work
	done        chan struct{}
	waitForWork bool
}

func newExecutor(r *reactor.Reactor) *Executor {
	e := &Executor{reactor: r, queue: make(chan *work, 64), done: make(chan struct{}), waitForWork: true}
	go e.main()
	return e
}

func (e *Executor) main() {
	defer close(e.done)
	for w := range e.queue {
		if w == nil {
			return
		}
		res, err := call(w.fn)
		e.reactor.AsyncComplete(w.completion, result{res, err})
	}
}

// call runs fn. A panic is an error, as the "except Exception" of the Python code.
func call(fn func() (any, error)) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("%v", r)
		}
	}()
	return fn()
}

// Submit runs fn in the worker goroutine, and waits for the result. After Join, it runs fn
// directly.
func (e *Executor) Submit(fn func() (any, error)) (any, error) {
	if !e.waitForWork {
		return fn()
	}
	completion := e.reactor.NewCompletion()
	e.queue <- &work{completion, fn}
	r := completion.Wait(reactor.Never, nil).(result)
	return r.res, r.err
}

// Join stops the worker goroutine.
func (e *Executor) Join() {
	e.waitForWork = false
	e.queue <- nil
	<-e.done
}

// Dispatcher is the aio_executor object. It stops the executors at the disconnect.
type Dispatcher struct {
	printer      *host.Printer
	reactor      *reactor.Reactor
	allExecutors []*Executor
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	d := &Dispatcher{printer: printer, reactor: printer.Reactor()}
	printer.RegisterEventHandler("klippy:disconnect", func(...any) { d.handleDisconnect() })
	return d
}

func init() { host.RegisterModule("aio_executor", load, nil) }

// Load returns the aio_executor object, and loads it the first time.
func Load(config *configfile.Wrapper) *Dispatcher {
	return host.Of(config).LoadObject(config, "aio_executor").(*Dispatcher)
}

func (d *Dispatcher) handleDisconnect() {
	for _, e := range d.allExecutors {
		e.Join()
	}
}

// AllocateExecutor starts an executor. The name is the thread name of the Python code.
func (d *Dispatcher) AllocateExecutor(name string) *Executor {
	e := newExecutor(d.reactor)
	d.allExecutors = append(d.allExecutors, e)
	return e
}

// Direct returns an executor that runs the work in the caller, for the tests.
func Direct() *Executor { return &Executor{} }
