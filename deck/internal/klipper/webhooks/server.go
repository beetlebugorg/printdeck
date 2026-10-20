package webhooks

// The Unix socket server of the API (ServerSocket and ClientConnection of webhooks.py).
//
// Klipper reads the sockets in its reactor. Here a goroutine reads each socket and gives the
// requests to the reactor as asynchronous callbacks, and a goroutine writes the responses. The
// requests run on the reactor, in the order that they come in.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

const requestLogSize = 20

type loggedRequest struct {
	eventtime float64
	request   string
}

// ServerSocket is the API server of the printer.
type ServerSocket struct {
	printer  *host.Printer
	webhooks *WebHooks
	reactor  *reactor.Reactor
	listener net.Listener
	mu       sync.Mutex // protects clients and nextUID
	clients  map[int]*ClientConnection
	nextUID  int
}

func newServerSocket(webhooks *WebHooks, printer *host.Printer) *ServerSocket {
	s := &ServerSocket{printer: printer, webhooks: webhooks, reactor: printer.Reactor(),
		clients: map[int]*ClientConnection{}}
	serverAddress, _ := printer.StartArgs().Get("apiserver")
	_, isFileinput := printer.StartArgs().Get("debuginput")
	if serverAddress == "" || isFileinput {
		// Do not enable server
		return s
	}
	s.removeSocketFile(serverAddress)
	l, err := net.Listen("unix", serverAddress)
	if err != nil {
		panic(fmt.Errorf("webhooks: unable to listen on '%s': %v", serverAddress, err))
	}
	s.listener = l
	go s.acceptLoop()
	printer.RegisterEventHandler("klippy:disconnect", func(...any) { s.handleDisconnect() })
	printer.RegisterEventHandler("klippy:analyze_shutdown", func(...any) { s.handleAnalyzeShutdown() })
	return s
}

func (s *ServerSocket) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		// The client starts on the reactor, as _handle_accept.
		s.reactor.RegisterAsyncCallback(func(float64) any {
			s.mu.Lock()
			s.nextUID++
			uid := s.nextUID
			s.mu.Unlock()
			client := newClientConnection(s, conn, uid)
			s.mu.Lock()
			s.clients[uid] = client
			s.mu.Unlock()
			go client.readLoop()
			go client.writeLoop()
			return nil
		}, reactor.Now)
	}
}

func (s *ServerSocket) clientList() []*ClientConnection {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ClientConnection, 0, len(s.clients))
	for i := 1; i <= s.nextUID; i++ {
		if c, ok := s.clients[i]; ok {
			out = append(out, c)
		}
	}
	return out
}

func (s *ServerSocket) handleDisconnect() {
	for _, client := range s.clientList() {
		client.Close()
	}
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *ServerSocket) handleAnalyzeShutdown() {
	for _, client := range s.clientList() {
		client.dumpRequestLog()
	}
}

func (s *ServerSocket) removeSocketFile(filePath string) {
	if err := os.Remove(filePath); err != nil {
		if _, serr := os.Stat(filePath); serr == nil {
			log.Printf("webhooks: Unable to delete socket file '%s'", filePath)
			panic(err)
		}
	}
}

func (s *ServerSocket) popClient(uid int) {
	s.mu.Lock()
	delete(s.clients, uid)
	s.mu.Unlock()
}

// Stats is called once per second. It closes the clients that do not read their responses.
func (s *ServerSocket) Stats(eventtime float64) (bool, string) {
	for _, client := range s.clientList() {
		if !client.blocking() {
			client.isBlocking = false
		} else {
			if !client.isBlocking {
				client.isBlocking = true
				client.blockingCount = 5
			}
			client.blockingCount--
			if client.blockingCount < 0 {
				log.Printf("Closing unresponsive client %d", client.uid)
				client.Close()
			}
		}
	}
	return false, ""
}

// ClientConnection is one client of the API server.
type ClientConnection struct {
	printer       *host.Printer
	webhooks      *WebHooks
	reactor       *reactor.Reactor
	server        *ServerSocket
	uid           int
	conn          net.Conn
	closed        bool
	partialData   []byte
	isBlocking    bool
	blockingCount int
	requestLog    []loggedRequest

	sendMu     sync.Mutex
	sendBuffer []byte
	writing    bool // the writer goroutine has data that the client did not read yet
	sendWake   chan struct{}
	done       chan struct{}
}

func newClientConnection(server *ServerSocket, conn net.Conn, uid int) *ClientConnection {
	c := &ClientConnection{printer: server.printer, webhooks: server.webhooks, reactor: server.reactor,
		server: server, uid: uid, conn: conn, sendWake: make(chan struct{}, 1), done: make(chan struct{})}
	c.SetClientInfo("?", "New connection")
	return c
}

func (c *ClientConnection) dumpRequestLog() {
	out := []string{fmt.Sprintf("Dumping %d requests for client %d", len(c.requestLog), c.uid)}
	for _, r := range c.requestLog {
		out = append(out, fmt.Sprintf("Received %f: %s", r.eventtime, r.request))
	}
	log.Print(strings.Join(out, "\n"))
}

// SetClientInfo logs the information of the client.
func (c *ClientConnection) SetClientInfo(clientInfo any, stateMsg string) {
	if stateMsg == "" {
		stateMsg = fmt.Sprintf("Client info %s", pyRepr(clientInfo))
	}
	log.Printf("webhooks client %d: %s", c.uid, stateMsg)
}

// pyRepr is a short form of Python's repr() for the log.
func pyRepr(v any) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Close closes the connection.
func (c *ClientConnection) Close() {
	if c.closed {
		return
	}
	c.SetClientInfo(nil, "Disconnected")
	c.closed = true
	close(c.done)
	c.conn.Close()
	c.server.popClient(c.uid)
}

// IsClosed tells if the connection is closed.
func (c *ClientConnection) IsClosed() bool { return c.closed }

// readLoop reads the requests, and runs each one on the reactor.
func (c *ClientConnection) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := c.conn.Read(buf)
		data := append([]byte(nil), buf[:n]...)
		c.reactor.RegisterAsyncCallback(func(eventtime float64) any {
			c.processReceived(eventtime, data, err != nil && n == 0)
			return nil
		}, reactor.Now)
		if err != nil && n == 0 {
			return
		}
	}
}

func (c *ClientConnection) processReceived(eventtime float64, data []byte, eof bool) {
	if c.closed {
		return
	}
	if eof {
		// Socket Closed
		c.Close()
		return
	}
	requests := bytes.Split(data, []byte{0x03})
	requests[0] = append(append([]byte(nil), c.partialData...), requests[0]...)
	c.partialData = requests[len(requests)-1]
	requests = requests[:len(requests)-1]
	for _, req := range requests {
		c.requestLog = append(c.requestLog, loggedRequest{eventtime, string(req)})
		if len(c.requestLog) > requestLogSize {
			c.requestLog = c.requestLog[1:]
		}
		webRequest, err := c.decodeRequest(req)
		if err != nil {
			log.Printf("webhooks: Error decoding Server Request %s: %v", req, err)
			continue
		}
		c.reactor.RegisterCallback(func(float64) any { c.processRequest(webRequest); return nil }, reactor.Now)
	}
}

// decodeRequest reads a request, as WebRequest.__init__ of webhooks.py.
func (c *ClientConnection) decodeRequest(req []byte) (*WebRequest, error) {
	dec := json.NewDecoder(bytes.NewReader(req))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	base, ok := pyNumbers(v).(map[string]any)
	if !ok {
		return nil, errors.New("Not a top-level dictionary")
	}
	method, ok := base["method"].(string)
	params := map[string]any{}
	if p, has := base["params"]; has {
		params, _ = p.(map[string]any)
	}
	if !ok || params == nil {
		return nil, errors.New("Invalid request type")
	}
	return NewWebRequest(c, base["id"], method, params), nil
}

// pyNumbers converts the numbers of a decoded JSON value to int64 and float64, as Python's json.
func pyNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil && !strings.ContainsAny(string(x), ".eE") {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = pyNumbers(e)
		}
	case []any:
		for i, e := range x {
			x[i] = pyNumbers(e)
		}
	}
	return v
}

func (c *ClientConnection) processRequest(webRequest *WebRequest) {
	result := c.webhooks.Call(webRequest)
	if result == nil {
		return
	}
	c.Send(result)
}

// Send sends a message to the client.
func (c *ClientConnection) Send(data map[string]any) {
	jmsg, err := json.Marshal(jsonSafe(data))
	if err != nil {
		msg := fmt.Sprintf("json encoding error: %v", err)
		log.Print(msg)
		c.printer.InvokeShutdown(msg, nil)
		return
	}
	if c.closed {
		return
	}
	c.sendMu.Lock()
	c.sendBuffer = append(append(c.sendBuffer, jmsg...), 0x03)
	c.sendMu.Unlock()
	select {
	case c.sendWake <- struct{}{}:
	default:
	}
}

// blocking tells if the client has not read all the responses. When it starts to block, the
// client has 5 more seconds.
func (c *ClientConnection) blocking() bool {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.writing
}

func (c *ClientConnection) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case <-c.sendWake:
		}
		for {
			c.sendMu.Lock()
			data := c.sendBuffer
			c.sendBuffer = nil
			c.writing = len(data) > 0
			c.sendMu.Unlock()
			if len(data) == 0 {
				break
			}
			c.conn.SetWriteDeadline(time.Now().Add(time.Second))
			_, err := c.conn.Write(data)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					// The client does not read. Keep the rest for the next write.
					c.sendMu.Lock()
					c.sendBuffer = append(data, c.sendBuffer...)
					c.sendMu.Unlock()
					continue
				}
				log.Printf("webhooks: socket write error %d", c.uid)
				c.reactor.RegisterAsyncCallback(func(float64) any { c.Close(); return nil }, reactor.Now)
				return
			}
		}
	}
}

// jsonSafe converts a value for encoding/json: NaN and the infinities are null, as msgspec writes
// them, and the arrays and maps of other types are converted too.
func jsonSafe(v any) any {
	switch x := v.(type) {
	case nil, bool, string, int, int64, json.Marshaler:
		return v
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonSafe(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonSafe(e)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return jsonSafe(rv.Float())
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = jsonSafe(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[fmt.Sprint(k.Interface())] = jsonSafe(rv.MapIndex(k).Interface())
		}
		return out
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
	}
	return v
}
