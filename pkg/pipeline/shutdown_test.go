package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func TestDispatcherStopCancelsInFlightAndAbandonsQueuedOnTimeout(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var handled sync.Map
	dispatcher, err := NewDispatcher(ruleengine.DispatchConfig{Workers: 1, QueueSize: 4},
		func(ctx context.Context, message ruleengine.Message) {
			handled.Store(message.ID, true)
			if message.ID != "slow" {
				return
			}
			close(started)
			<-ctx.Done()
			close(cancelled)
		})
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}

	for _, id := range []string{"slow", "queued-1", "queued-2"} {
		if err := dispatcher.Submit(context.Background(), ruleengine.Message{ID: id}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = dispatcher.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop error = %v, want deadline exceeded", err)
	}

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatalf("in-flight handler context was not cancelled")
	}
	// The workers exit once the abandoned events are skipped.
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	if _, ok := handled.Load("queued-1"); ok {
		t.Fatalf("queued event was handled after the stop deadline")
	}
	stats := dispatcher.Stats()
	if stats.Abandoned != 2 || stats.Processed != 1 {
		t.Fatalf("stats = %+v, want 2 abandoned and 1 processed", stats)
	}
}

func TestDispatcherHandlerContextKeepsValuesAndIgnoresSubmitterCancel(t *testing.T) {
	type key struct{}
	got := make(chan context.Context, 1)
	dispatcher := newTestDispatcher(t, ruleengine.DispatchConfig{Workers: 1},
		func(ctx context.Context, _ ruleengine.Message) { got <- ctx })

	submitCtx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	if err := dispatcher.Submit(submitCtx, ruleengine.Message{ID: "m1"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	cancel()
	handlerCtx := <-got
	if handlerCtx.Value(key{}) != "v" {
		t.Fatalf("handler context lost submitter values")
	}
	stopDispatcher(t, dispatcher)
	if handlerCtx.Err() == nil {
		// The handler has returned, so its context is released.
		t.Fatalf("handler context should be released after the handler returns")
	}
}

func TestEngineStopWaitsForInFlightMessages(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	exec := &funcExecutor{fn: func(context.Context, int) error {
		close(started)
		<-release
		return nil
	}}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{}, ruleengine.Action{}, exec)

	processed := make(chan error, 1)
	go func() {
		_, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{ID: "m1"})
		processed <- err
	}()
	<-started

	stopped := make(chan error, 1)
	go func() { stopped <- engine.Stop(context.Background()) }()

	waitForEngineState(t, engine, engineStateStopping)
	// New messages are rejected while Stop waits.
	if _, err := engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{ID: "m2"}); err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("process during stop error = %v, want not started", err)
	}
	select {
	case err := <-stopped:
		t.Fatalf("stop returned before the in-flight message finished: %v", err)
	default:
	}

	close(release)
	if err := <-processed; err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestEngineStopHonorsContextWhileMessagesAreInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	exec := &funcExecutor{fn: func(context.Context, int) error {
		close(started)
		<-release // ignores ctx on purpose
		return nil
	}}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{}, ruleengine.Action{}, exec)

	processed := make(chan struct{})
	go func() {
		_, _ = engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{ID: "m1"})
		close(processed)
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := engine.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop error = %v, want deadline exceeded", err)
	}
	if err := engine.Start(context.Background()); err == nil {
		t.Fatalf("start should fail while components are pending cleanup")
	}

	close(release)
	<-processed
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("retry stop: %v", err)
	}
}

func TestEngineStopRejectsConcurrentTransition(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	exec := &funcExecutor{fn: func(context.Context, int) error {
		close(started)
		<-release
		return nil
	}}
	engine := newExecutionEngine(t, ruleengine.ExecutionConfig{}, ruleengine.Action{}, exec)
	go func() {
		_, _ = engine.Process(context.Background(), ruleengine.DefaultWorkflowName, ruleengine.Message{ID: "m1"})
	}()
	<-started

	stopped := make(chan error, 1)
	go func() { stopped <- engine.Stop(context.Background()) }()
	waitForEngineState(t, engine, engineStateStopping)
	if err := engine.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot stop") {
		t.Fatalf("concurrent stop error = %v, want cannot stop", err)
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func waitForEngineState(t *testing.T, engine *Engine, want engineState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		engine.mu.RLock()
		state := engine.state
		engine.mu.RUnlock()
		if state == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("engine state = %d, want %d", state, want)
		}
		time.Sleep(time.Millisecond)
	}
}
