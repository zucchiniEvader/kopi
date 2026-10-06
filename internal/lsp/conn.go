// Package lsp is a client of the Language Server Protocol: JSON-RPC over a
// language server's standard input and output, and the parts of the
// protocol the editors use.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Handler takes what the server sends unasked: notifications, and
// requests, which it answers with a result or an error. The connection
// calls it on goroutines of its own, in the order notifications come.
type Handler interface {
	Notify(method string, params json.RawMessage)
	Request(method string, params json.RawMessage) (any, error)
}

// ErrClosed is the error of calls on a closed connection.
var ErrClosed = errors.New("lsp: connection closed")

// Error is an error the server answered a request with.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("lsp: %s (%d)", e.Message, e.Code) }

// Conn is a JSON-RPC connection to a language server.
type Conn struct {
	w       io.Writer
	closer  io.Closer
	handler Handler

	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *message
	closed  bool
	done    chan struct{}
	err     error

	// notes keeps notifications in order, apart from the reader, so that a
	// slow handler holds no response back.
	notes chan *message
}

// message is any JSON-RPC message: a request (ID and Method), a
// notification (Method), or a response (ID, and Result or Error).
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// NewConn starts a connection over rwc, the server's output to read and
// its input to write, and closes rwc once the connection closes.
func NewConn(rwc io.ReadWriteCloser, h Handler) *Conn {
	c := &Conn{w: rwc, closer: rwc, handler: h, pending: map[int64]chan *message{}, done: make(chan struct{}), notes: make(chan *message, 256)}
	go c.read(bufio.NewReader(rwc))
	go c.notify()
	return c
}

// Done is closed once the connection closes, by Close or because the
// server went away; Err then says why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection closed, nil while it is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close closes the connection, failing the calls waiting.
func (c *Conn) Close() error {
	c.shut(ErrClosed)
	return nil
}

func (c *Conn) shut(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed, c.err = true, err
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	c.closer.Close()
	for _, ch := range pending {
		close(ch)
	}
	close(c.done)
}

// Call sends a request and decodes its result into result, unless nil.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	id, ch, err := c.start(method, params)
	if err != nil {
		return err
	}
	return c.wait(ctx, id, ch, result)
}

// Go sends a request at once, after what was sent before, and returns
// where its error comes once its result is decoded into result: requests
// and notifications reach the server in the order they are made.
func (c *Conn) Go(ctx context.Context, method string, params, result any) <-chan error {
	done := make(chan error, 1)
	id, ch, err := c.start(method, params)
	if err != nil {
		done <- err
		return done
	}
	go func() { done <- c.wait(ctx, id, ch, result) }()
	return done
}

func (c *Conn) start(method string, params any) (int64, chan *message, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, nil, ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan *message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	raw := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.send(&message{ID: &raw, Method: method, Params: marshal(params)}); err != nil {
		c.forget(id)
		return 0, nil, err
	}
	return id, ch, nil
}

func (c *Conn) wait(ctx context.Context, id int64, ch chan *message, result any) error {
	select {
	case m, ok := <-ch:
		if !ok {
			return ErrClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if result != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		// The server may stop working on it.
		c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	}
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	return c.send(&message{Method: method, Params: marshal(params)})
}

func marshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func (c *Conn) send(m *message) error {
	m.JSONRPC = "2.0"
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = c.w.Write(data)
	return err
}

// read reads messages until the server's output ends.
func (c *Conn) read(r *bufio.Reader) {
	defer close(c.notes)
	for {
		m, err := readMessage(r)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				err = ErrClosed
			}
			c.shut(err)
			return
		}
		switch {
		case m.ID != nil && m.Method != "":
			go c.answer(m)
		case m.ID != nil:
			id, err := strconv.ParseInt(string(*m.ID), 10, 64)
			if err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case m.Method != "":
			select {
			case c.notes <- m:
			case <-c.done:
				return
			}
		}
	}
}

// notify hands the notifications to the handler, in order.
func (c *Conn) notify() {
	for m := range c.notes {
		if c.handler != nil {
			c.handler.Notify(m.Method, m.Params)
		}
	}
}

// answer answers a request of the server.
func (c *Conn) answer(m *message) {
	var result any
	var err error
	if c.handler != nil {
		result, err = c.handler.Request(m.Method, m.Params)
	} else {
		err = &Error{Code: -32601, Message: "method not found: " + m.Method}
	}
	reply := &message{ID: m.ID}
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			e = &Error{Code: -32603, Message: err.Error()}
		}
		reply.Error = e
	} else {
		reply.Result = marshal(result)
		if reply.Result == nil {
			reply.Result = json.RawMessage("null")
		}
	}
	c.send(reply)
}

// readMessage reads a message: headers, of which Content-Length counts, a
// blank line, and the content.
func readMessage(r *bufio.Reader) (*message, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", value)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("lsp: a message without Content-Length")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	m := &message{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("lsp: %w", err)
	}
	return m, nil
}
