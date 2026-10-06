package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeServer answers requests on the other end of a pipe, as a server.
type fakeServer struct {
	r *bufio.Reader
	w io.Writer
}

func (s *fakeServer) read(t *testing.T) *message {
	t.Helper()
	m, err := readMessage(s.r)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (s *fakeServer) send(t *testing.T, m *message) {
	t.Helper()
	m.JSONRPC = "2.0"
	data, _ := json.Marshal(m)
	if _, err := io.WriteString(s.w, "Content-Length: "+strconv.Itoa(len(data))+"\r\n\r\n"+string(data)); err != nil {
		t.Fatal(err)
	}
}

type recorder struct {
	mu    sync.Mutex
	notes []string
	got   chan string
}

func (r *recorder) Notify(method string, params json.RawMessage) {
	r.mu.Lock()
	r.notes = append(r.notes, method)
	r.mu.Unlock()
	r.got <- method
}

func (r *recorder) Request(method string, params json.RawMessage) (any, error) {
	if method == "workspace/configuration" {
		return []any{nil}, nil
	}
	return nil, &Error{Code: -32601, Message: "no " + method}
}

func pipe(t *testing.T, h Handler) (*Conn, *fakeServer) {
	client, server := net.Pipe()
	t.Cleanup(func() { server.Close() })
	return NewConn(client, h), &fakeServer{r: bufio.NewReader(server), w: server}
}

func TestCallAndNotify(t *testing.T) {
	rec := &recorder{got: make(chan string, 10)}
	c, s := pipe(t, rec)
	defer c.Close()
	done := make(chan error, 1)
	var result struct{ Answer int }
	go func() { done <- c.Call(context.Background(), "test/ask", map[string]int{"q": 1}, &result) }()
	req := s.read(t)
	if req.Method != "test/ask" || string(req.Params) != `{"q":1}` {
		t.Fatalf("request %s %s", req.Method, req.Params)
	}
	// A notification and a request of the server's come before the answer.
	s.send(t, &message{Method: "window/logMessage", Params: json.RawMessage(`{"message":"hi"}`)})
	id := json.RawMessage(`"srv-1"`)
	s.send(t, &message{ID: &id, Method: "workspace/configuration", Params: json.RawMessage(`{}`)})
	reply := s.read(t)
	if string(*reply.ID) != `"srv-1"` || string(reply.Result) != `[null]` {
		t.Errorf("reply %s %s", *reply.ID, reply.Result)
	}
	s.send(t, &message{ID: req.ID, Result: json.RawMessage(`{"Answer":42}`)})
	if err := <-done; err != nil || result.Answer != 42 {
		t.Fatalf("call: %v, %+v", err, result)
	}
	if m := <-rec.got; m != "window/logMessage" {
		t.Errorf("notification %q", m)
	}
	// An error answered.
	go func() { done <- c.Call(context.Background(), "test/fail", nil, nil) }()
	req = s.read(t)
	s.send(t, &message{ID: req.ID, Error: &Error{Code: -32000, Message: "nope"}})
	var e *Error
	if err := <-done; !errors.As(err, &e) || e.Message != "nope" {
		t.Errorf("error %v", err)
	}
	// Notifications from the client.
	go c.Notify("initialized", struct{}{})
	if n := s.read(t); n.Method != "initialized" || n.ID != nil {
		t.Errorf("notification %+v", n)
	}
}

func TestCloseFailsCalls(t *testing.T) {
	c, s := pipe(t, nil)
	done := make(chan error, 1)
	go func() { done <- c.Call(context.Background(), "test/never", nil, nil) }()
	s.read(t)
	s.w.(net.Conn).Close() // the server goes away
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the call waits on")
	}
	<-c.Done()
	if err := c.Call(context.Background(), "test/after", nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call after closing: %v", err)
	}
}

func TestUTF16(t *testing.T) {
	line := "a世😀b"
	// a: 1 byte, 1 unit; 世: 3 bytes, 1 unit; 😀: 4 bytes, 2 units.
	for _, c := range []struct{ col, ch int }{{0, 0}, {1, 1}, {4, 2}, {8, 4}, {9, 5}} {
		if got := UTF16Col(line, c.col); got != c.ch {
			t.Errorf("UTF16Col(%d) = %d, want %d", c.col, got, c.ch)
		}
		if got := ByteCol(line, c.ch); got != c.col {
			t.Errorf("ByteCol(%d) = %d, want %d", c.ch, got, c.col)
		}
	}
	if got := ByteCol(line, 99); got != len(line) {
		t.Errorf("past the end: %d", got)
	}
}

func TestURI(t *testing.T) {
	p := "/Users/ada/my project/Main.java"
	uri := FileURI(p)
	if uri != "file:///Users/ada/my%20project/Main.java" {
		t.Errorf("uri %s", uri)
	}
	if PathOf(uri) != p {
		t.Errorf("path %s", PathOf(uri))
	}
	if PathOf("jdt://contents/rt.jar/java.lang/String.class") != "" {
		t.Error("a jdt URI has a path")
	}
}

func TestHoverText(t *testing.T) {
	h := Hover{Contents: json.RawMessage(`[{"language":"java","value":"String java.lang.String.trim()"},"Returns a string whose value is this string, with **all** leading and trailing [space](https://x) removed.\n\n* Returns:\n  * a \u0060string\u0060\n\nSource: *Java 21*"]`)}
	want := "String java.lang.String.trim()\n\nReturns a string whose value is this string, with all leading and trailing space removed.\n\n* Returns:\n  * a string\n\nSource: Java 21"
	if got := h.Text(); got != want {
		t.Errorf("text\n%q\nwant\n%q", got, want)
	}
	h = Hover{Contents: json.RawMessage(`{"kind":"markdown","value":"` + "```java\\nint x\\n```" + `"}`)}
	if got := h.Text(); got != "int x" {
		t.Errorf("markup %q", got)
	}
}
