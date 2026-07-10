package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// AdminServer owns a fail-fast HTTP listener with graceful shutdown.
type AdminServer struct {
	mu       sync.Mutex
	address  string
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	done     chan error
	options  AdminServerOptions
}

// AdminServerOptions configures HTTP request and connection timeouts.
type AdminServerOptions struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	Listen            func(network, address string) (net.Listener, error)
}

// DefaultAdminServerOptions returns conservative defaults for an admin API.
func DefaultAdminServerOptions() AdminServerOptions {
	return AdminServerOptions{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// NewAdminServer constructs an admin server without binding its listener.
func NewAdminServer(address string, handler http.Handler, options ...AdminServerOptions) *AdminServer {
	serverOptions := DefaultAdminServerOptions()
	if len(options) > 0 {
		serverOptions = options[0]
	}
	return &AdminServer{address: address, handler: handler, options: serverOptions}
}

// Start binds the configured address before launching the HTTP serve loop.
func (s *AdminServer) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return fmt.Errorf("admin server is already started")
	}

	listen := s.options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", s.address)
	if err != nil {
		return err
	}

	s.listener = listener
	s.server = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.options.ReadHeaderTimeout,
		ReadTimeout:       s.options.ReadTimeout,
		WriteTimeout:      s.options.WriteTimeout,
		IdleTimeout:       s.options.IdleTimeout,
	}
	s.done = make(chan error, 1)

	go func() {
		err := s.server.Serve(listener)
		if err == http.ErrServerClosed {
			err = nil
		}
		s.done <- err
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server and returns any serve error.
func (s *AdminServer) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}

	err := s.server.Shutdown(ctx)
	serveStopped := false
	if s.done != nil {
		select {
		case serveErr := <-s.done:
			serveStopped = true
			if err == nil {
				err = serveErr
			}
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	if serveStopped {
		s.server = nil
		s.listener = nil
		s.done = nil
	}
	return err
}

// Addr returns the bound listener address after Start succeeds.
func (s *AdminServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
