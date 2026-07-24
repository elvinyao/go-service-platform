package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type testFakeAPIManager struct {
	startErr       error
	stopErr        error
	started        bool
	stopped        bool
	printed        bool
	confluencePort int
	mmHTTPPort     int
	mmWSPort       int
	wsPort         int
}

func (m *testFakeAPIManager) Start() error {
	m.started = true
	return m.startErr
}

func (m *testFakeAPIManager) Stop() error {
	m.stopped = true
	return m.stopErr
}

func (m *testFakeAPIManager) PrintEndpoints() {
	m.printed = true
}

func TestRunStartsPrintsAndStopsAfterSignal(t *testing.T) {
	manager := &testFakeAPIManager{}
	restore := replaceManagerFactory(func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
		manager.confluencePort = confluencePort
		manager.mmHTTPPort = mmHTTPPort
		manager.mmWSPort = mmWSPort
		manager.wsPort = wsPort
		return manager
	})
	defer restore()

	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt

	err := run([]string{
		"-confluence-port", "19090",
		"-mm-http-port", "19091",
		"-mm-ws-port", "19092",
		"-ws-port", "19093",
	}, signals, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !manager.started || !manager.printed || !manager.stopped {
		t.Fatalf("manager state = started:%v printed:%v stopped:%v", manager.started, manager.printed, manager.stopped)
	}
	if manager.confluencePort != 19090 || manager.mmHTTPPort != 19091 || manager.mmWSPort != 19092 || manager.wsPort != 19093 {
		t.Fatalf("ports = %d/%d/%d/%d", manager.confluencePort, manager.mmHTTPPort, manager.mmWSPort, manager.wsPort)
	}
}

func TestRunReturnsStartAndFlagErrors(t *testing.T) {
	startErr := fmt.Errorf("boom")
	restore := replaceManagerFactory(func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
		return &testFakeAPIManager{startErr: startErr}
	})
	defer restore()

	err := run(nil, make(chan os.Signal), &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected start error")
	}

	err = run([]string{"-unknown"}, make(chan os.Signal), &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected flag parse error")
	}
}

func TestRunReturnsStopError(t *testing.T) {
	stopErr := fmt.Errorf("stop failed")
	restore := replaceManagerFactory(func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
		return &testFakeAPIManager{stopErr: stopErr}
	})
	defer restore()

	signals := make(chan os.Signal, 1)
	signals <- os.Interrupt
	if err := run(nil, signals, &bytes.Buffer{}); err == nil {
		t.Fatalf("expected stop error")
	}
}

func TestRunHealthcheckDoesNotStartManager(t *testing.T) {
	managerCreated := false
	restoreFactory := replaceManagerFactory(func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
		managerCreated = true
		return &testFakeAPIManager{}
	})
	defer restoreFactory()

	previousHealthCheck := fakeServerHealthCheck
	defer func() { fakeServerHealthCheck = previousHealthCheck }()
	checkedPort := 0
	fakeServerHealthCheck = func(port int) error {
		checkedPort = port
		return nil
	}

	if err := run([]string{"-healthcheck", "-ws-port", "19093"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatalf("run healthcheck: %v", err)
	}
	if managerCreated {
		t.Fatalf("healthcheck created the fake API manager")
	}
	if checkedPort != 19093 {
		t.Fatalf("healthcheck port = %d, want 19093", checkedPort)
	}

	checkErr := fmt.Errorf("not ready")
	fakeServerHealthCheck = func(int) error { return checkErr }
	if err := run([]string{"-healthcheck"}, nil, &bytes.Buffer{}); err != checkErr {
		t.Fatalf("healthcheck error = %v, want %v", err, checkErr)
	}
}

func TestCheckFakeServerHealthValidatesHTTPStatus(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port

	if err := checkFakeServerHealth(port); err != nil {
		t.Fatalf("healthy server: %v", err)
	}
	status.Store(http.StatusServiceUnavailable)
	if err := checkFakeServerHealth(port); err == nil {
		t.Fatalf("unhealthy server error = nil")
	}
	server.Close()
	if err := checkFakeServerHealth(port); err == nil {
		t.Fatalf("closed server error = nil")
	}
}

func TestMainHandlesInterrupt(t *testing.T) {
	manager := &testFakeAPIManager{}
	restoreFactory := replaceManagerFactory(func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager {
		return manager
	})
	defer restoreFactory()

	previousArgs := os.Args
	os.Args = []string{"fake-server"}
	defer func() { os.Args = previousArgs }()

	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send interrupt: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("main did not return after interrupt")
	}
	if !manager.started || !manager.stopped {
		t.Fatalf("manager state = started:%v stopped:%v", manager.started, manager.stopped)
	}
}

func replaceManagerFactory(factory func(confluencePort, mmHTTPPort, mmWSPort, wsPort int) fakeAPIManager) func() {
	previous := newFakeAPIManagerWithPorts
	newFakeAPIManagerWithPorts = factory
	return func() {
		newFakeAPIManagerWithPorts = previous
	}
}
