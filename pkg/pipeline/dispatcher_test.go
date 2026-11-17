package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func newTestDispatcher(t *testing.T, config ruleengine.DispatchConfig, handler Handler) *Dispatcher {
	t.Helper()
	dispatcher, err := NewDispatcher(config, handler)
	if err != nil {
		t.Fatalf("new dispatcher: %v", err)
	}
	t.Cleanup(func() { _ = dispatcher.Stop(context.Background()) })
	return dispatcher
}

func stopDispatcher(t *testing.T, dispatcher *Dispatcher) {
	t.Helper()
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("stop dispatcher: %v", err)
	}
}

func TestNewDispatcherValidatesInput(t *testing.T) {
	if _, err := NewDispatcher(ruleengine.DispatchConfig{}, nil); err == nil {
		t.Fatalf("expected nil handler error")
	}
	if _, err := NewDispatcher(ruleengine.DispatchConfig{Workers: -1}, func(context.Context, ruleengine.Message) {}); err == nil {
		t.Fatalf("expected invalid config error")
	}
}

func TestDispatcherProcessesEventsConcurrently(t *testing.T) {
	const workers = 4
	release := make(chan struct{})
	started := make(chan struct{}, workers)
	dispatcher := newTestDispatcher(t, ruleengine.DispatchConfig{Workers: workers},
		func(context.Context, ruleengine.Message) {
			started <- struct{}{}
			<-release
		})

	for i := 0; i < workers; i++ {
		if err := dispatcher.Submit(context.Background(), ruleengine.Message{ID: fmt.Sprint(i)}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	// Every worker must be inside the handler at the same time.
	for i := 0; i < workers; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d events started concurrently", i, workers)
		}
	}
	close(release)
	stopDispatcher(t, dispatcher)

	stats := dispatcher.Stats()
	if stats.Workers != workers || stats.Accepted != workers || stats.Processed != workers || stats.Queued != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestDispatcherKeepsOrderPerOrderingKey(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]string{}
	dispatcher := newTestDispatcher(t,
		ruleengine.DispatchConfig{Workers: 8, OrderingKey: "payload.channel.id"},
		func(_ context.Context, message ruleengine.Message) {
			channel, _ := message.Field("payload.channel.id")
			mu.Lock()
			seen[channel.(string)] = append(seen[channel.(string)], message.ID)
			mu.Unlock()
		})

	const perChannel = 200
	channels := []string{"C1", "C2", "C3", "C4", "C5"}
	for i := 0; i < perChannel; i++ {
		for _, channel := range channels {
			err := dispatcher.Submit(context.Background(), ruleengine.Message{
				ID:      fmt.Sprintf("%s-%04d", channel, i),
				Payload: map[string]interface{}{"channel": map[string]interface{}{"id": channel}},
			})
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
	}
	stopDispatcher(t, dispatcher)

	for _, channel := range channels {
		ids := seen[channel]
		if len(ids) != perChannel {
			t.Fatalf("channel %s processed %d events, want %d", channel, len(ids), perChannel)
		}
		for i, id := range ids {
			if want := fmt.Sprintf("%s-%04d", channel, i); id != want {
				t.Fatalf("channel %s position %d = %s, want %s", channel, i, id, want)
			}
		}
	}
}

func TestDispatcherSpreadsEventsWithoutOrderingKey(t *testing.T) {
	dispatcher := newTestDispatcher(t,
		ruleengine.DispatchConfig{Workers: 2, OrderingKey: "payload.channel.id"},
		func(context.Context, ruleengine.Message) {})

	first := dispatcher.queueIndex(ruleengine.Message{})
	second := dispatcher.queueIndex(ruleengine.Message{Payload: map[string]interface{}{"channel": map[string]interface{}{"id": ""}}})
	if first == second {
		t.Fatalf("events without a key should rotate across workers, got %d twice", first)
	}
}

func TestDispatcherDropsDuplicateEventIDs(t *testing.T) {
	var processed atomic.Int32
	dispatcher := newTestDispatcher(t,
		ruleengine.DispatchConfig{Dedup: ruleengine.DedupConfig{Enabled: true}},
		func(context.Context, ruleengine.Message) { processed.Add(1) })

	if err := dispatcher.Submit(context.Background(), ruleengine.Message{ID: "evt-1"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if err := dispatcher.Submit(context.Background(), ruleengine.Message{ID: "evt-1"}); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("second submit error = %v, want ErrDuplicateEvent", err)
	}
	// Events without an ID cannot be deduplicated.
	for i := 0; i < 2; i++ {
		if err := dispatcher.Submit(nil, ruleengine.Message{}); err != nil { //nolint:staticcheck // nil ctx is tolerated
			t.Fatalf("submit without id: %v", err)
		}
	}
	stopDispatcher(t, dispatcher)

	if got := processed.Load(); got != 3 {
		t.Fatalf("processed = %d, want 3", got)
	}
	if stats := dispatcher.Stats(); stats.Duplicates != 1 || stats.Accepted != 3 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestDispatcherDropModeRejectsWhenQueueIsFull(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	dispatcher := newTestDispatcher(t,
		ruleengine.DispatchConfig{
			Workers: 1, QueueSize: 1, QueueFull: ruleengine.QueueFullDrop,
			Dedup: ruleengine.DedupConfig{Enabled: true},
		},
		func(context.Context, ruleengine.Message) {
			started <- struct{}{}
			<-release
		})

	ctx := context.Background()
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "running"}); err != nil {
		t.Fatalf("submit running: %v", err)
	}
	<-started
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "queued"}); err != nil {
		t.Fatalf("submit queued: %v", err)
	}
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "dropped"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("error = %v, want ErrQueueFull", err)
	}
	if stats := dispatcher.Stats(); stats.Dropped != 1 || stats.Queued != 1 {
		t.Fatalf("stats = %+v", stats)
	}

	close(release)
	<-started
	// A dropped event was never processed, so its redelivery must be accepted.
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "dropped"}); err != nil {
		t.Fatalf("resubmit dropped event: %v", err)
	}
}

func TestDispatcherBlockModeAppliesBackpressure(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	dispatcher := newTestDispatcher(t,
		ruleengine.DispatchConfig{Workers: 1, QueueSize: 1},
		func(context.Context, ruleengine.Message) {
			started <- struct{}{}
			<-release
		})

	if err := dispatcher.Submit(context.Background(), ruleengine.Message{}); err != nil {
		t.Fatalf("submit running: %v", err)
	}
	<-started
	if err := dispatcher.Submit(context.Background(), ruleengine.Message{}); err != nil {
		t.Fatalf("submit queued: %v", err)
	}

	// The queue is full: a submit waits, and gives up when its context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := dispatcher.Submit(ctx, ruleengine.Message{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}

	// A blocked submit is released by Stop instead of racing queue shutdown.
	blocked := make(chan error, 1)
	go func() { blocked <- dispatcher.Submit(context.Background(), ruleengine.Message{}) }()
	time.Sleep(10 * time.Millisecond)
	stopped := make(chan error, 1)
	go func() { stopped <- dispatcher.Stop(context.Background()) }()
	if err := <-blocked; !errors.Is(err, ErrDispatcherStopped) {
		t.Fatalf("blocked submit error = %v, want ErrDispatcherStopped", err)
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := dispatcher.Submit(context.Background(), ruleengine.Message{}); !errors.Is(err, ErrDispatcherStopped) {
		t.Fatalf("submit after stop error = %v, want ErrDispatcherStopped", err)
	}
	if stats := dispatcher.Stats(); stats.Processed != 2 {
		t.Fatalf("stats = %+v, want both accepted events drained", stats)
	}
}

func TestDispatcherStopHonorsContextWhileDraining(t *testing.T) {
	release := make(chan struct{})
	dispatcher := newTestDispatcher(t, ruleengine.DispatchConfig{Workers: 1},
		func(context.Context, ruleengine.Message) { <-release })
	if err := dispatcher.Submit(context.Background(), ruleengine.Message{}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := dispatcher.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := dispatcher.Stop(nil); err != nil { //nolint:staticcheck // nil ctx is tolerated
		t.Fatalf("second stop: %v", err)
	}
}

func TestDispatcherSurvivesHandlerPanicAndDetachesCancellation(t *testing.T) {
	type keyType struct{}
	results := make(chan error, 1)
	dispatcher := newTestDispatcher(t, ruleengine.DispatchConfig{Workers: 1},
		func(ctx context.Context, message ruleengine.Message) {
			if message.ID == "panic" {
				panic("handler bug")
			}
			if ctx.Value(keyType{}) != "kept" {
				results <- errors.New("context values were lost")
				return
			}
			results <- ctx.Err()
		})

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), keyType{}, "kept"))
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "panic"}); err != nil {
		t.Fatalf("submit panic: %v", err)
	}
	if err := dispatcher.Submit(ctx, ruleengine.Message{ID: "ok"}); err != nil {
		t.Fatalf("submit ok: %v", err)
	}
	cancel()
	if err := <-results; err != nil {
		t.Fatalf("handler context: %v", err)
	}
	stopDispatcher(t, dispatcher)
	if stats := dispatcher.Stats(); stats.Panics != 1 || stats.Processed != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestDedupStoreExpiresAndBoundsEntries(t *testing.T) {
	now := time.Unix(0, 0)
	store := newDedupStore(time.Minute, 2, func() time.Time { return now })

	if !store.claim("a") || store.claim("a") {
		t.Fatalf("first claim should succeed and second should be a duplicate")
	}
	now = now.Add(time.Minute)
	if !store.claim("a") {
		t.Fatalf("claim after the TTL should succeed")
	}

	// The size bound forgets the oldest ID first.
	now = now.Add(time.Second)
	store.claim("b")
	now = now.Add(time.Second)
	store.claim("c")
	if !store.claim("a") {
		t.Fatalf("oldest id should have been evicted by the size bound")
	}
	if store.claim("c") {
		t.Fatalf("recent id should still be remembered")
	}

	// A forgotten ID can be claimed again and is not evicted by its stale entry.
	store.forget("c")
	now = now.Add(time.Second)
	if !store.claim("c") || store.claim("c") {
		t.Fatalf("forgotten id should be claimable exactly once")
	}
}
