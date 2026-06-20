package runtime

import (
	"context"
	"net"
	"net/http"
	"time"
)

type AdminServer struct {
	address  string
	handler  http.Handler
	server   *http.Server
	listener net.Listener
	done     chan error
}

func NewAdminServer(address string, handler http.Handler) *AdminServer {
	return &AdminServer{address: address, handler: handler}
}

func (s *AdminServer) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}

	s.listener = listener
	s.server = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
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

func (s *AdminServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	err := s.server.Shutdown(ctx)
	if s.done != nil {
		select {
		case serveErr := <-s.done:
			if err == nil {
				err = serveErr
			}
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
		}
	}
	return err
}

func (s *AdminServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}
