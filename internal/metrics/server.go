package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server serves Registry.Handler on a TCP address (typically ":9090" or "127.0.0.1:9090").
type Server struct {
	addr     string
	registry *Registry
	httpSrv  *http.Server
	ln       net.Listener
}

// Listen starts an HTTP server exposing GET /metrics. addr empty → no-op (nil, nil).
func Listen(addr string, registry *Registry) (*Server, error) {
	if addr == "" {
		return nil, nil
	}
	if registry == nil {
		return nil, errors.New("metrics: registry is required when metrics-listen is set")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("metrics: listen %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", registry.Handler())
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s := &Server{addr: addr, registry: registry, httpSrv: srv, ln: ln}
	go func() { _ = srv.Serve(ln) }()
	return s, nil
}

// Addr returns the bound address (useful when listen used ":0").
func (s *Server) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Shutdown stops the metrics HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}
