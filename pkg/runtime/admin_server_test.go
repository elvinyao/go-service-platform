package runtime

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestAdminServerStartFailsWhenAddressIsInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	server := NewAdminServer(listener.Addr().String(), http.NewServeMux())
	err = server.Start(context.Background())
	if err == nil {
		t.Fatalf("expected address in use error")
	}
}

func TestAdminServerStartsAndStops(t *testing.T) {
	server := NewAdminServer("127.0.0.1:0", http.NewServeMux())
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	if server.Addr() == "" {
		t.Fatalf("server address is empty")
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("stop server: %v", err)
	}
}

func TestAdminServerAddrBeforeStartAndStopBeforeStart(t *testing.T) {
	server := NewAdminServer("127.0.0.1:0", http.NewServeMux())
	if server.Addr() != "" {
		t.Fatalf("addr = %q, want empty before start", server.Addr())
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
}
