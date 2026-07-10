package fakeapi

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
)

type listenFunc func(network, address string) (net.Listener, error)

func startHTTPServer(server *http.Server, name string, listen listenFunc) error {
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen for %s on %s: %w", name, server.Addr, err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("%s server error: %v", name, err)
		}
	}()
	return nil
}
