// Package testhttp provides a FakeHttpServer-class peer for transport tests
// (parity with tracewick/tracewick tests/Support/FakeHttpServer): raw TCP actions
// that httptest cannot express (close-before-read, stall-then-hang-up, truncated).
package testhttp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Action is one accepted connection. Mirrors the PHP fake-http-server scenario.
type Action struct {
	// Status answers with HTTP/1.1 <status> after reading the request (default 200 when Raw empty).
	Status int
	// Raw, when non-nil, is written as the complete response bytes after reading the request.
	Raw []byte
	// Stall waits this long after reading the request (and before writing, if any).
	Stall time.Duration
	// Close closes the connection immediately without reading (PHP {"close": true}).
	Close bool
	// HangUpAfterStall closes without writing a response after Stall (PHP stall without status).
	HangUpAfterStall bool
}

// Server listens on 127.0.0.1 and plays Actions in order, then stops accepting.
type Server struct {
	ln   net.Listener
	url  string
	done chan struct{}
	wg   sync.WaitGroup
}

// Start listens and serves actions. Call Close when finished.
func Start(actions []Action) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		ln:   ln,
		url:  "http://" + ln.Addr().String(),
		done: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.serve(actions)
	return s, nil
}

// URL is the http://127.0.0.1:<port> base (no path).
func (s *Server) URL() string { return s.url }

// Close stops the listener and waits for the serve loop.
func (s *Server) Close() {
	_ = s.ln.Close()
	<-s.done
	s.wg.Wait()
}

func (s *Server) serve(actions []Action) {
	defer close(s.done)
	defer s.wg.Done()
	for _, action := range actions {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		handle(conn, action)
	}
	// Drain until Close so late dials fail cleanly.
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func handle(conn net.Conn, action Action) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if action.Close {
		return
	}

	if err := readHTTPRequest(conn); err != nil {
		return
	}

	if action.Stall > 0 {
		time.Sleep(action.Stall)
	}
	if action.HangUpAfterStall {
		return
	}
	if action.Raw != nil {
		_, _ = conn.Write(action.Raw)
		return
	}
	status := action.Status
	if status == 0 {
		status = 200
	}
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d Test\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", status)
}

func readHTTPRequest(conn net.Conn) error {
	br := bufio.NewReader(conn)
	var header strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		header.WriteString(line)
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	h := header.String()
	length := 0
	for _, line := range strings.Split(h, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err == nil {
				length = n
			}
		}
	}
	if length > 0 {
		_, err := io.CopyN(io.Discard, br, int64(length))
		return err
	}
	return nil
}
