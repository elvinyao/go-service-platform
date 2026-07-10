package concurrency

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewWorkerPoolRequiresPositiveWorkers(t *testing.T) {
	if _, err := NewWorkerPool(0); err == nil {
		t.Fatalf("zero worker error = nil")
	}
}

func TestWorkerPoolRunsEveryTaskWithinConcurrencyLimit(t *testing.T) {
	pool := newTestWorkerPool(t, 2)
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32

	tasks := make([]Task, 6)
	for i := range tasks {
		tasks[i] = func() error {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			completed.Add(1)
			return nil
		}
	}

	if err := pool.Run(context.Background(), tasks...); err != nil {
		t.Fatalf("run tasks: %v", err)
	}
	if completed.Load() != int32(len(tasks)) {
		t.Fatalf("completed = %d, want %d", completed.Load(), len(tasks))
	}
	if maximum.Load() > 2 {
		t.Fatalf("maximum concurrency = %d, want at most 2", maximum.Load())
	}
}

func TestWorkerPoolAggregatesTaskErrorsInInputOrder(t *testing.T) {
	firstErr := errors.New("first failure")
	thirdErr := errors.New("third failure")
	err := newTestWorkerPool(t, 3).Run(
		context.Background(),
		func() error { return firstErr },
		func() error { return nil },
		func() error { return thirdErr },
	)
	if !errors.Is(err, firstErr) || !errors.Is(err, thirdErr) {
		t.Fatalf("joined error = %v, want both task errors", err)
	}
	if first := strings.Index(err.Error(), "task 0"); first < 0 || first > strings.Index(err.Error(), "task 2") {
		t.Fatalf("task errors are not input ordered: %v", err)
	}
}

func TestWorkerPoolCancellationStopsScheduling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	err := newTestWorkerPool(t, 2).Run(ctx,
		func() error { calls.Add(1); return nil },
		func() error { calls.Add(1); return nil },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("task calls = %d, want 0", calls.Load())
	}
}

func TestWorkerPoolValidatesInputsAndEmptyBatch(t *testing.T) {
	pool := newTestWorkerPool(t, 1)
	if err := pool.Run(nil); err == nil || err.Error() != "context is nil" {
		t.Fatalf("nil context error = %v", err)
	}
	if err := pool.Run(context.Background(), nil); err == nil || err.Error() != "task 0 is nil" {
		t.Fatalf("nil task error = %v", err)
	}
	if err := pool.Run(context.Background()); err != nil {
		t.Fatalf("empty batch error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pool.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("empty canceled batch error = %v", err)
	}
}

func TestWorkerPoolDoesNotLoseFastTaskFailures(t *testing.T) {
	tasks := make([]Task, 50)
	for index := range tasks {
		index := index
		tasks[index] = func() error {
			return fmt.Errorf("failure-%d", index)
		}
	}

	err := newTestWorkerPool(t, 8).Run(context.Background(), tasks...)
	for index := range tasks {
		if !strings.Contains(err.Error(), fmt.Sprintf("failure-%d", index)) {
			t.Fatalf("joined error missing failure-%d: %v", index, err)
		}
	}
}

func newTestWorkerPool(t *testing.T, workers int) *WorkerPool {
	t.Helper()
	pool, err := NewWorkerPool(workers)
	if err != nil {
		t.Fatalf("new worker pool: %v", err)
	}
	return pool
}
