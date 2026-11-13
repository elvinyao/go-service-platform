package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type memoryAddr string

func (a memoryAddr) Network() string { return "memory" }
func (a memoryAddr) String() string  { return string(a) }

type blockingListener struct {
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener(address string) *blockingListener {
	return &blockingListener{addr: memoryAddr(address), closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *blockingListener) Addr() net.Addr { return l.addr }

func memoryListen(network, address string) (net.Listener, error) {
	return newBlockingListener(address), nil
}

func TestAdminServerStartFailsWhenAddressIsInUse(t *testing.T) {
	want := errors.New("address already in use")
	options := DefaultAdminServerOptions()
	options.Listen = func(string, string) (net.Listener, error) { return nil, want }

	server := NewAdminServer("memory:8080", http.NewServeMux(), options)
	err := server.Start(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("start error = %v, want %v", err, want)
	}
}

func TestAdminServerUsesConfiguredTimeouts(t *testing.T) {
	options := AdminServerOptions{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		Listen:            memoryListen,
	}
	server := NewAdminServer("memory:8080", http.NewServeMux(), options)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer server.Stop(context.Background())

	if server.server.ReadHeaderTimeout != time.Second || server.server.ReadTimeout != 2*time.Second ||
		server.server.WriteTimeout != 3*time.Second || server.server.IdleTimeout != 4*time.Second {
		t.Fatalf("server timeouts = %+v", server.server)
	}
}

func TestAdminServerStartsAndStops(t *testing.T) {
	options := DefaultAdminServerOptions()
	options.Listen = memoryListen
	server := NewAdminServer("memory:8080", http.NewServeMux(), options)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	if server.Addr() == "" {
		t.Fatalf("server address is empty")
	}
	if err := server.Start(context.Background()); err == nil {
		t.Fatalf("second start error = nil")
	}
	if err := server.Stop(nil); err != nil {
		t.Fatalf("stop server: %v", err)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("second stop server: %v", err)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("restart server: %v", err)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("stop restarted server: %v", err)
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

func TestAdminServerStartHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := DefaultAdminServerOptions()
	options.Listen = func(string, string) (net.Listener, error) {
		t.Fatalf("listen must not be called for canceled context")
		return nil, nil
	}
	server := NewAdminServer("memory:8080", http.NewServeMux(), options)

	if err := server.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("start error = %v, want context canceled", err)
	}
}

type failingListener struct {
	err      error
	accepted chan struct{}
}

func (l *failingListener) Accept() (net.Conn, error) {
	close(l.accepted)
	return nil, l.err
}
func (*failingListener) Close() error   { return nil }
func (*failingListener) Addr() net.Addr { return memoryAddr("memory:failed") }

func TestAdminServerStopReturnsServeError(t *testing.T) {
	want := errors.New("accept failed")
	listener := &failingListener{err: want, accepted: make(chan struct{})}
	options := DefaultAdminServerOptions()
	options.Listen = func(string, string) (net.Listener, error) {
		return listener, nil
	}
	server := NewAdminServer("memory:failed", http.NewServeMux(), options)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	<-listener.accepted

	if err := server.Stop(context.Background()); !errors.Is(err, want) {
		t.Fatalf("stop error = %v, want %v", err, want)
	}
}

func TestAdminServerDrainingStateBlocksStartUntilRetryStopCompletes(t *testing.T) {
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-releaseHandler
		w.WriteHeader(http.StatusOK)
	})

	server := NewAdminServer("127.0.0.1:0", mux)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("start server: %v", err)
	}
	addr := server.Addr()
	if addr == "" {
		t.Fatalf("expected non-empty addr while serving")
	}

	reqDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if resp != nil {
			_ = resp.Body.Close()
		}
		reqDone <- err
	}()
	<-handlerStarted

	shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := server.Stop(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop timeout error = %v, want deadline exceeded", err)
	}
	if server.Addr() != "" {
		t.Fatalf("addr during draining = %q, want empty", server.Addr())
	}
	if err := server.Start(context.Background()); err == nil {
		t.Fatalf("expected start error while server is draining")
	}

	close(releaseHandler)
	if err := <-reqDone; err != nil {
		t.Fatalf("in-flight request error: %v", err)
	}

	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("retry stop after handler finished: %v", err)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("restart after drain completed: %v", err)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("final stop: %v", err)
	}
}
