package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type adminServerState uint8

const (
	adminStateStopped adminServerState = iota
	adminStateServing
	adminStateDraining
)

type adminServeSession struct {
	done chan struct{}
	err  error
}

// AdminServer owns a fail-fast HTTP listener with graceful shutdown.
type AdminServer struct {
	mu       sync.Mutex
	state    adminServerState
	address  string
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	session  *adminServeSession
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
	switch s.state {
	case adminStateServing:
		return fmt.Errorf("admin server is already started")
	case adminStateDraining:
		return fmt.Errorf("admin server is shutting down")
	}

	listen := s.options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", s.address)
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: s.options.ReadHeaderTimeout,
		ReadTimeout:       s.options.ReadTimeout,
		WriteTimeout:      s.options.WriteTimeout,
		IdleTimeout:       s.options.IdleTimeout,
	}
	session := &adminServeSession{done: make(chan struct{})}

	s.listener = listener
	s.server = server
	s.session = session
	s.state = adminStateServing

	go func(srv *http.Server, ln net.Listener, sess *adminServeSession) {
		err := srv.Serve(ln)
		if err == http.ErrServerClosed {
			err = nil
		}
		sess.err = err
		close(sess.done)
	}(server, listener, session)

	return nil
}

// Stop gracefully shuts down the HTTP server and returns any serve error.
func (s *AdminServer) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	switch s.state {
	case adminStateStopped:
		s.mu.Unlock()
		return nil
	case adminStateServing, adminStateDraining:
		s.state = adminStateDraining
		s.listener = nil
		server := s.server
		session := s.session
		s.mu.Unlock()

		var shutdownErr error
		if server != nil {
			shutdownErr = server.Shutdown(ctx)
		}
		return s.awaitServeDrain(ctx, session, shutdownErr)
	default:
		s.mu.Unlock()
		return nil
	}
}

func (s *AdminServer) awaitServeDrain(ctx context.Context, session *adminServeSession, shutdownErr error) error {
	// If Shutdown timed out or was cancelled, in-flight HTTP handlers may still be active;
	// remain in adminStateDraining until a subsequent Stop observes clean completion.
	if shutdownErr != nil {
		return shutdownErr
	}
	if session == nil {
		return nil
	}

	select {
	case <-session.done:
		s.mu.Lock()
		if s.session == session {
			s.server = nil
			s.listener = nil
			s.session = nil
			s.state = adminStateStopped
		}
		s.mu.Unlock()
		return session.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Addr returns the bound listener address while the server is actively serving.
func (s *AdminServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != adminStateServing || s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
