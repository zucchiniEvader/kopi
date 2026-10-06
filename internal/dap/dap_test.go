package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

type recorder struct {
	events chan string
}

func (r *recorder) Event(event string, body json.RawMessage) { r.events <- event + " " + string(body) }

func (r *recorder) Request(command string, args json.RawMessage) (any, error) {
	if command == "runInTerminal" {
		return map[string]int{"processId": 42}, nil
	}
	return nil, errors.New("no")
}

func write(t *testing.T, w io.Writer, m *Message) {
	t.Helper()
	data, _ := json.Marshal(m)
	if _, err := io.WriteString(w, "Content-Length: "+strconv.Itoa(len(data))+"\r\n\r\n"+string(data)); err != nil {
		t.Fatal(err)
	}
}

func TestClient(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	rec := &recorder{events: make(chan string, 10)}
	c := New(client, rec)
	defer c.Close()
	// The pipe waits for a reader: the adapter's side reads apart.
	msgs := make(chan *Message, 10)
	go func() {
		r := bufio.NewReader(server)
		for {
			m, err := readMessage(r)
			if err != nil {
				close(msgs)
				return
			}
			msgs <- m
		}
	}()
	next := func() (*Message, error) {
		select {
		case m, ok := <-msgs:
			if !ok {
				return nil, io.EOF
			}
			return m, nil
		case <-time.After(2 * time.Second):
			return nil, errors.New("no message")
		}
	}

	var body struct{ SupportsConfigurationDoneRequest bool }
	done := c.Go(context.Background(), "initialize", map[string]string{"adapterID": "java"}, &body)
	req, err := next()
	if err != nil || req.Command != "initialize" || req.Type != "request" {
		t.Fatalf("request %+v, %v", req, err)
	}
	// An event and a reverse request before the response.
	write(t, server, &Message{Seq: 1, Type: "event", Event: "initialized"})
	write(t, server, &Message{Seq: 2, Type: "request", Command: "runInTerminal", Arguments: json.RawMessage(`{"args":["java"]}`)})
	reply, err := next()
	if err != nil || reply.Type != "response" || reply.RequestSeq != 2 || !reply.Success || string(reply.Body) != `{"processId":42}` {
		t.Fatalf("reply %+v, %v", reply, err)
	}
	write(t, server, &Message{Seq: 3, Type: "response", RequestSeq: req.Seq, Command: "initialize", Success: true, Body: json.RawMessage(`{"supportsConfigurationDoneRequest":true}`)})
	if err := <-done; err != nil || !body.SupportsConfigurationDoneRequest {
		t.Fatalf("initialize: %v, %+v", err, body)
	}
	if e := <-rec.events; e != "initialized " {
		t.Errorf("event %q", e)
	}
	// A failure.
	done = c.Go(context.Background(), "next", map[string]int{"threadId": 1}, nil)
	req, _ = next()
	write(t, server, &Message{Seq: 4, Type: "response", RequestSeq: req.Seq, Command: "next", Success: false, Message: "not stopped"})
	var e *Error
	if err := <-done; !errors.As(err, &e) || e.Message != "not stopped" {
		t.Errorf("error %v", err)
	}
	// The adapter going away fails what waits.
	done = c.Go(context.Background(), "continue", nil, nil)
	next()
	server.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waits on")
	}
}
