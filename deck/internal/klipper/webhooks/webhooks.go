// Package webhooks is the API of the Klipper host: the endpoints that the modules register, the
// G-code endpoints, and the status queries and subscriptions of the printer objects.
//
// Ported to Go from klippy/webhooks.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The Unix socket server (ServerSocket,
// ClientConnection) is in server.go.
//
//	Copyright (C) 2020 Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package webhooks

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// WebRequestError is the error of a request. It is a command error, as in Klipper.
type WebRequestError = gcode.CommandError

func requestError(format string, args ...any) *WebRequestError { return gcode.Errorf(format, args...) }

// ClientConn is the connection of a client, for the responses that come later (subscriptions and
// remote methods).
type ClientConn interface {
	Send(data map[string]any)
	IsClosed() bool
	SetClientInfo(info any, stateMsg string)
}

// WebRequest is one request to an endpoint.
type WebRequest struct {
	clientConn ClientConn
	ID         any
	method     string
	params     map[string]any
	response   any
	isError    bool
}

// NewWebRequest makes a request. id nil is a request with no response.
func NewWebRequest(conn ClientConn, id any, method string, params map[string]any) *WebRequest {
	if params == nil {
		params = map[string]any{}
	}
	return &WebRequest{clientConn: conn, ID: id, method: method, params: params}
}

func (r *WebRequest) ClientConnection() ClientConn { return r.clientConn }
func (r *WebRequest) Method() string               { return r.method }

// Get returns a parameter. With no default, a missing parameter is an error.
func (r *WebRequest) Get(item string, def ...any) any {
	v, ok := r.params[item]
	if !ok {
		if len(def) == 0 {
			panic(requestError("Missing Argument [%s]", item))
		}
		return def[0]
	}
	return v
}

func (r *WebRequest) typed(item string, check func(any) bool, def []any) any {
	v := r.Get(item, def...)
	if _, ok := r.params[item]; ok && !check(v) {
		panic(requestError("Invalid Argument Type [%s]", item))
	}
	return v
}

// GetStr returns a string parameter.
func (r *WebRequest) GetStr(item string, def ...any) any {
	return r.typed(item, func(v any) bool { _, ok := v.(string); return ok }, def)
}

// GetInt returns an integer parameter.
func (r *WebRequest) GetInt(item string, def ...any) any {
	return r.typed(item, func(v any) bool {
		switch x := v.(type) {
		case int, int64:
			return true
		case float64:
			// A JSON number with no fraction is an int in Python.
			return x == float64(int64(x))
		}
		return false
	}, def)
}

// GetFloat returns a number parameter.
func (r *WebRequest) GetFloat(item string, def ...any) float64 {
	v := r.typed(item, func(v any) bool {
		switch v.(type) {
		case int, int64, float64:
			return true
		}
		return false
	}, def)
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	panic(requestError("Invalid Argument Type [%s]", item))
}

// GetDict returns a dictionary parameter, or nil for a nil default.
func (r *WebRequest) GetDict(item string, def ...any) map[string]any {
	v := r.typed(item, func(v any) bool { _, ok := v.(map[string]any); return ok }, def)
	d, _ := v.(map[string]any)
	return d
}

// SetError sets the error response.
func (r *WebRequest) SetError(err *WebRequestError) {
	r.isError = true
	r.response = map[string]any{"error": "WebRequestError", "message": err.Msg}
}

// Send sets the response.
func (r *WebRequest) Send(data any) {
	if r.response != nil {
		panic(requestError("Multiple calls to send not allowed"))
	}
	r.response = data
}

// Finish returns the response message, or nil for a request with no id.
func (r *WebRequest) Finish() map[string]any {
	if r.ID == nil {
		return nil
	}
	rtype := "result"
	if r.isError {
		rtype = "error"
	}
	if r.response == nil {
		// No error was set and the user never executed send, default response is {}
		r.response = map[string]any{}
	}
	return map[string]any{"id": r.ID, rtype: r.response}
}

// Handler handles a request.
type Handler func(req *WebRequest)

type mux struct {
	key    string
	values map[string]Handler
	hasNil bool
}

// WebHooks holds the endpoints.
type WebHooks struct {
	printer       *host.Printer
	endpoints     map[string]Handler
	endpointOrder []string
	remoteMethods map[string]map[ClientConn]map[string]any
	muxEndpoints  map[string]*mux
	sconn         *ServerSocket
}

func newWebHooks(printer *host.Printer) *WebHooks {
	w := &WebHooks{printer: printer, endpoints: map[string]Handler{},
		remoteMethods: map[string]map[ClientConn]map[string]any{}, muxEndpoints: map[string]*mux{}}
	w.endpoints["list_endpoints"] = w.handleListEndpoints
	w.endpointOrder = append(w.endpointOrder, "list_endpoints")
	w.RegisterEndpoint("info", w.handleInfoRequest)
	w.RegisterEndpoint("emergency_stop", func(*WebRequest) {
		printer.InvokeShutdown("Shutdown due to webhooks request", nil)
	})
	w.RegisterEndpoint("register_remote_method", w.handleRPCRegistration)
	w.sconn = newServerSocket(w, printer)
	return w
}

// Connection is the API server.
func (w *WebHooks) Connection() *ServerSocket { return w.sconn }

// Stats closes the clients that do not read their responses.
func (w *WebHooks) Stats(eventtime float64) (bool, string) { return w.sconn.Stats(eventtime) }

func init() {
	host.EarlyObjects = append(host.EarlyObjects, func(p *host.Printer) {
		p.AddObject("webhooks", newWebHooks(p))
		newGCodeHelper(p)
		newQueryStatusHelper(p)
	})
}

// Of returns the webhooks object of a printer.
func Of(p *host.Printer) *WebHooks { return p.LookupObject("webhooks").(*WebHooks) }

// RegisterEndpoint adds an endpoint.
func (w *WebHooks) RegisterEndpoint(path string, callback Handler) {
	if _, ok := w.endpoints[path]; ok {
		panic(requestError("Path already registered to an endpoint"))
	}
	w.endpoints[path] = callback
	w.endpointOrder = append(w.endpointOrder, path)
}

// RegisterMuxEndpoint adds an endpoint that runs the handler of a value of its key parameter. A
// nil value is the handler for no key.
func (w *WebHooks) RegisterMuxEndpoint(path, key string, value *string, callback Handler) {
	prev, ok := w.muxEndpoints[path]
	if !ok {
		w.RegisterEndpoint(path, w.handleMux)
		prev = &mux{key: key, values: map[string]Handler{}}
		w.muxEndpoints[path] = prev
	}
	vs := "None"
	if value != nil {
		vs = *value
	}
	if prev.key != key {
		configfile.Raise("mux endpoint %s %s %s may have only one key (%s)", path, key, vs, prev.key)
	}
	k := "\x00none"
	if value != nil {
		k = *value
	}
	if _, ok := prev.values[k]; ok {
		configfile.Raise("mux endpoint %s %s %s already registered", path, key, vs)
	}
	if value == nil {
		prev.hasNil = true
	}
	prev.values[k] = callback
}

func (w *WebHooks) handleMux(req *WebRequest) {
	m := w.muxEndpoints[req.Method()]
	var keyParam any
	if m.hasNil {
		keyParam = req.Get(m.key, nil)
	} else {
		keyParam = req.Get(m.key)
	}
	k, isStr := keyParam.(string)
	if keyParam == nil {
		k, isStr = "\x00none", true
	}
	fn, ok := m.values[k]
	if !isStr || !ok {
		panic(requestError("The value '%v' is not valid for %s", keyParam, m.key))
	}
	fn(req)
}

func (w *WebHooks) handleListEndpoints(req *WebRequest) {
	req.Send(map[string]any{"endpoints": append([]string(nil), w.endpointOrder...)})
}

func (w *WebHooks) handleInfoRequest(req *WebRequest) {
	if clientInfo := req.GetDict("client_info", nil); clientInfo != nil && req.ClientConnection() != nil {
		req.ClientConnection().SetClientInfo(clientInfo, "")
	}
	stateMessage, state := w.printer.StateMessage()
	hostname, _ := os.Hostname()
	exe, _ := os.Executable()
	response := map[string]any{"state": state, "state_message": stateMessage, "hostname": hostname,
		"klipper_path": exe, "python_path": exe, "process_id": os.Getpid(), "user_id": os.Getuid(),
		"group_id": os.Getgid()}
	for _, sa := range []string{"log_file", "config_file", "software_version", "cpu_info"} {
		response[sa] = w.printer.StartArgs()[sa]
	}
	req.Send(response)
}

func (w *WebHooks) handleRPCRegistration(req *WebRequest) {
	template := req.GetDict("response_template")
	method, _ := req.GetStr("remote_method").(string)
	conn := req.ClientConnection()
	log.Printf("webhooks: registering remote method '%s' for connection %p", method, conn)
	if w.remoteMethods[method] == nil {
		w.remoteMethods[method] = map[ClientConn]map[string]any{}
	}
	w.remoteMethods[method][conn] = template
}

// Callback returns the handler of an endpoint. A missing endpoint is a request error.
func (w *WebHooks) Callback(path string) Handler {
	cb, ok := w.endpoints[path]
	if !ok {
		msg := fmt.Sprintf("webhooks: No registered callback for path '%s'", path)
		log.Print(msg)
		panic(requestError("%s", msg))
	}
	return cb
}

// Call runs a request on the reactor, as ClientConnection._process_request, and returns the
// response message (nil for a request with no id).
func (w *WebHooks) Call(req *WebRequest) map[string]any {
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if e, ok := r.(*gcode.CommandError); ok {
				req.SetError(e)
				return
			}
			msg := "Internal Error on WebRequest: " + req.Method()
			log.Printf("%s: %v\n%s", msg, r, debug.Stack())
			req.SetError(requestError("%v", r))
			w.printer.InvokeShutdown(msg, nil)
		}()
		w.Callback(req.Method())(req)
	}()
	return req.Finish()
}

// Status returns the state of the printer.
func (w *WebHooks) Status(eventtime float64) map[string]any {
	stateMessage, state := w.printer.StateMessage()
	return map[string]any{"state": state, "state_message": stateMessage}
}

// CallRemoteMethod sends a call to the clients that registered a remote method.
func (w *WebHooks) CallRemoteMethod(method string, kwargs map[string]any) {
	connMap, ok := w.remoteMethods[method]
	if !ok {
		panic(gcode.Errorf("Remote method '%s' not registered", method))
	}
	valid := map[ClientConn]map[string]any{}
	for conn, template := range connMap {
		if !conn.IsClosed() {
			valid[conn] = template
			out := map[string]any{"params": kwargs}
			for k, v := range template {
				out[k] = v
			}
			conn.Send(out)
		}
	}
	if len(valid) == 0 {
		delete(w.remoteMethods, method)
		panic(gcode.Errorf("No active connections for method '%s'", method))
	}
	w.remoteMethods[method] = valid
}

// gcodeHelper has the G-code endpoints.
type gcodeHelper struct {
	printer            *host.Printer
	gcode              *gcode.Dispatch
	isOutputRegistered bool
	clients            map[ClientConn]map[string]any
}

func newGCodeHelper(printer *host.Printer) {
	h := &gcodeHelper{printer: printer, gcode: gcode.Of(printer), clients: map[ClientConn]map[string]any{}}
	wh := Of(printer)
	wh.RegisterEndpoint("gcode/help", func(req *WebRequest) { req.Send(h.gcode.CommandHelp()) })
	wh.RegisterEndpoint("gcode/script", func(req *WebRequest) {
		script, _ := req.GetStr("script").(string)
		h.gcode.RunScript(script)
	})
	wh.RegisterEndpoint("gcode/restart", func(*WebRequest) { h.gcode.RunScript("restart") })
	wh.RegisterEndpoint("gcode/firmware_restart", func(*WebRequest) { h.gcode.RunScript("firmware_restart") })
	wh.RegisterEndpoint("gcode/subscribe_output", h.handleSubscribeOutput)
}

func (h *gcodeHelper) outputCallback(msg string) {
	for conn, template := range h.clients {
		if conn.IsClosed() {
			delete(h.clients, conn)
			continue
		}
		tmp := map[string]any{}
		for k, v := range template {
			tmp[k] = v
		}
		tmp["params"] = map[string]any{"response": msg}
		conn.Send(tmp)
	}
}

func (h *gcodeHelper) handleSubscribeOutput(req *WebRequest) {
	conn := req.ClientConnection()
	template := req.GetDict("response_template", map[string]any{})
	h.clients[conn] = template
	if !h.isOutputRegistered {
		h.gcode.RegisterOutputHandler(h.outputCallback)
		h.isOutputRegistered = true
	}
}

const subscriptionRefreshTime = .25

// Statuser is a printer object with a status.
type Statuser interface {
	Status(eventtime float64) map[string]any
}

type subscription struct {
	conn     ClientConn
	objects  map[string][]string // nil items: all items
	allItems map[string]bool
	send     func(map[string]any)
	template map[string]any
}

// queryStatusHelper has the status endpoints.
type queryStatusHelper struct {
	printer        *host.Printer
	clients        map[ClientConn]*subscription
	clientOrder    []ClientConn
	pendingQueries []*subscription
	queryTimer     *reactor.Timer
	lastQuery      map[string]map[string]any
}

func newQueryStatusHelper(printer *host.Printer) {
	q := &queryStatusHelper{printer: printer, clients: map[ClientConn]*subscription{},
		lastQuery: map[string]map[string]any{}}
	wh := Of(printer)
	wh.RegisterEndpoint("objects/list", q.handleList)
	wh.RegisterEndpoint("objects/query", func(req *WebRequest) { q.handleQuery(req, false) })
	wh.RegisterEndpoint("objects/subscribe", func(req *WebRequest) { q.handleQuery(req, true) })
}

func (q *queryStatusHelper) handleList(req *WebRequest) {
	objects := []string{}
	for _, o := range q.printer.LookupObjects("") {
		if _, ok := o.Obj.(Statuser); ok {
			objects = append(objects, o.Name)
		}
	}
	req.Send(map[string]any{"objects": objects})
}

func equal(a, b any) bool { return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b) }

func (q *queryStatusHelper) doQuery(eventtime float64) float64 {
	lastQuery := q.lastQuery
	query := map[string]map[string]any{}
	q.lastQuery = query
	msglist := q.pendingQueries
	q.pendingQueries = nil
	for _, c := range q.clientOrder {
		msglist = append(msglist, q.clients[c])
	}
	r := q.printer.Reactor()
	r.AssertNoPause(func() {
		for _, s := range msglist {
			isQuery := s.conn == nil
			if !isQuery && s.conn.IsClosed() {
				q.removeClient(s.conn)
				continue
			}
			// Query each requested printer object
			cquery := map[string]any{}
			for objName, reqItems := range s.objects {
				res, ok := query[objName]
				if !ok {
					if po, ok := q.printer.TryLookupObject(objName).(Statuser); ok {
						res = po.Status(eventtime)
					} else {
						res = map[string]any{}
					}
					query[objName] = res
				}
				if reqItems == nil && s.allItems[objName] {
					reqItems = make([]string, 0, len(res))
					for k := range res {
						reqItems = append(reqItems, k)
					}
					if len(reqItems) > 0 {
						s.objects[objName] = reqItems
						delete(s.allItems, objName)
					}
				}
				lres := lastQuery[objName]
				cres := map[string]any{}
				for _, ri := range reqItems {
					rd := res[ri]
					if isQuery || !equal(rd, lres[ri]) {
						cres[ri] = rd
					}
				}
				if len(cres) > 0 || isQuery {
					cquery[objName] = cres
				}
			}
			// Send data
			if len(cquery) > 0 || isQuery {
				tmp := map[string]any{}
				for k, v := range s.template {
					tmp[k] = v
				}
				tmp["params"] = map[string]any{"eventtime": eventtime, "status": cquery}
				s.send(tmp)
			}
		}
	})
	if len(query) == 0 {
		// Unregister timer if there are no longer any subscriptions
		r.UnregisterTimer(q.queryTimer)
		q.queryTimer = nil
		return reactor.Never
	}
	return eventtime + subscriptionRefreshTime
}

func (q *queryStatusHelper) removeClient(conn ClientConn) {
	delete(q.clients, conn)
	for i, c := range q.clientOrder {
		if c == conn {
			q.clientOrder = append(q.clientOrder[:i], q.clientOrder[i+1:]...)
			break
		}
	}
}

func (q *queryStatusHelper) handleQuery(req *WebRequest, isSubscribe bool) {
	raw := req.GetDict("objects")
	// Validate subscription format
	objects := map[string][]string{}
	allItems := map[string]bool{}
	for k, v := range raw {
		if v == nil {
			objects[k] = nil
			allItems[k] = true
			continue
		}
		list, ok := v.([]any)
		if !ok {
			panic(requestError("Invalid argument"))
		}
		items := []string{}
		for _, ri := range list {
			s, ok := ri.(string)
			if !ok {
				panic(requestError("Invalid argument"))
			}
			items = append(items, s)
		}
		objects[k] = items
	}
	// Add to pending queries
	conn := req.ClientConnection()
	template := req.GetDict("response_template", map[string]any{})
	if isSubscribe && conn != nil {
		if _, ok := q.clients[conn]; ok {
			q.removeClient(conn)
		}
	}
	r := q.printer.Reactor()
	complete := r.NewCompletion()
	q.pendingQueries = append(q.pendingQueries, &subscription{objects: objects, allItems: allItems,
		send: func(m map[string]any) { complete.Complete(m) }, template: map[string]any{}})
	// Start timer if needed
	if q.queryTimer == nil {
		q.queryTimer = r.RegisterTimer(q.doQuery, reactor.Now)
	}
	// Wait for data to be queried
	msg := complete.Wait(reactor.Never, nil).(map[string]any)
	req.Send(msg["params"])
	if isSubscribe && conn != nil {
		subObjects := map[string][]string{}
		subAll := map[string]bool{}
		for k, v := range objects {
			subObjects[k] = v
		}
		for k := range allItems {
			subAll[k] = true
		}
		q.clients[conn] = &subscription{conn: conn, objects: subObjects, allItems: subAll, send: conn.Send,
			template: template}
		q.clientOrder = append(q.clientOrder, conn)
	}
}
