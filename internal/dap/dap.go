// Package dap is a client of the Debug Adapter Protocol: requests,
// responses and events over a debug adapter's connection, framed as the
// Language Server Protocol frames them.
package dap

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

// ErrClosed is the error of requests on a closed connection.
var ErrClosed = errors.New("dap: connection closed")

// Handler takes what the adapter sends unasked: events, in order, and
// reverse requests, as runInTerminal, which it answers.
type Handler interface {
	Event(event string, body json.RawMessage)
	Request(command string, args json.RawMessage) (any, error)
}

// Message is any message of the protocol.
type Message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"` // request, response or event
	Command    string          `json:"command,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Event      string          `json:"event,omitempty"`
}

// Error is a request the adapter failed.
type Error struct {
	Command, Message string
}

func (e *Error) Error() string { return fmt.Sprintf("dap: %s: %s", e.Command, e.Message) }

// Client is a connection to a debug adapter.
type Client struct {
	w       io.Writer
	closer  io.Closer
	handler Handler

	wmu     sync.Mutex
	mu      sync.Mutex
	seq     int
	pending map[int]chan *Message
	closed  bool
	done    chan struct{}
	events  chan *Message
}

// New starts a client over rwc, which it closes as it closes.
func New(rwc io.ReadWriteCloser, h Handler) *Client {
	c := &Client{w: rwc, closer: rwc, handler: h, pending: map[int]chan *Message{}, done: make(chan struct{}), events: make(chan *Message, 1024)}
	go c.read(bufio.NewReader(rwc))
	go c.dispatch()
	return c
}

// Done is closed once the connection closes.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the connection, failing the requests waiting.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	c.closer.Close()
	for _, ch := range pending {
		close(ch)
	}
	close(c.done)
	return nil
}

// Go sends a request at once, after those sent before, and returns where
// its error comes once its body is decoded into result, unless nil.
func (c *Client) Go(ctx context.Context, command string, args, result any) <-chan error {
	done := make(chan error, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		done <- ErrClosed
		return done
	}
	c.seq++
	seq := c.seq
	ch := make(chan *Message, 1)
	c.pending[seq] = ch
	c.mu.Unlock()
	m := &Message{Seq: seq, Type: "request", Command: command}
	if args != nil {
		m.Arguments, _ = json.Marshal(args)
	}
	if err := c.send(m); err != nil {
		c.forget(seq)
		done <- err
		return done
	}
	go func() {
		select {
		case r, ok := <-ch:
			switch {
			case !ok:
				done <- ErrClosed
			case !r.Success:
				done <- &Error{Command: command, Message: r.Message}
			case result != nil && len(r.Body) > 0:
				done <- json.Unmarshal(r.Body, result)
			default:
				done <- nil
			}
		case <-ctx.Done():
			c.forget(seq)
			done <- ctx.Err()
		}
	}()
	return done
}

// Call sends a request and waits for its response.
func (c *Client) Call(ctx context.Context, command string, args, result any) error {
	return <-c.Go(ctx, command, args, result)
}

func (c *Client) forget(seq int) {
	c.mu.Lock()
	delete(c.pending, seq)
	c.mu.Unlock()
}

func (c *Client) send(m *Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = c.w.Write(data)
	return err
}

func (c *Client) read(r *bufio.Reader) {
	defer close(c.events)
	for {
		m, err := readMessage(r)
		if err != nil {
			c.Close()
			return
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[m.RequestSeq]
			delete(c.pending, m.RequestSeq)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case "event":
			select {
			case c.events <- m:
			case <-c.done:
				return
			}
		case "request":
			go c.answer(m)
		}
	}
}

func (c *Client) dispatch() {
	for m := range c.events {
		if c.handler != nil {
			c.handler.Event(m.Event, m.Body)
		}
	}
}

// answer answers a reverse request of the adapter.
func (c *Client) answer(m *Message) {
	r := &Message{Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: true}
	var body any
	var err error
	if c.handler != nil {
		body, err = c.handler.Request(m.Command, m.Arguments)
	} else {
		err = fmt.Errorf("unsupported: %s", m.Command)
	}
	if err != nil {
		r.Success, r.Message = false, err.Error()
	} else if body != nil {
		r.Body, _ = json.Marshal(body)
	}
	c.mu.Lock()
	c.seq++
	r.Seq = c.seq
	c.mu.Unlock()
	c.send(r)
}

func readMessage(r *bufio.Reader) (*Message, error) {
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
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("dap: bad Content-Length %q", value)
			}
		}
	}
	if length < 0 {
		return nil, errors.New("dap: a message without Content-Length")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	m := &Message{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("dap: %w", err)
	}
	return m, nil
}

// The protocol's types the client reads.

type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
	// SourceReference names a source the adapter gives, as a class of a
	// library, with the source request; 0 for a file.
	SourceReference int `json:"sourceReference,omitempty"`
}

type StackFrame struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Source *Source `json:"source,omitempty"`
	Line   int     `json:"line"`
	Column int     `json:"column"`
}

type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive"`
}

type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

type StoppedEvent struct {
	Reason            string `json:"reason"`
	Description       string `json:"description"`
	ThreadID          int    `json:"threadId"`
	AllThreadsStopped bool   `json:"allThreadsStopped"`
	Text              string `json:"text"`
}

type OutputEvent struct {
	Category string `json:"category"`
	Output   string `json:"output"`
}

// RunInTerminal is the reverse request of an adapter asking the client to
// start the program, as java-debug asks with a console other than its
// own.
type RunInTerminal struct {
	Kind  string            `json:"kind"`
	Title string            `json:"title"`
	Cwd   string            `json:"cwd"`
	Args  []string          `json:"args"`
	Env   map[string]string `json:"env"`
}
