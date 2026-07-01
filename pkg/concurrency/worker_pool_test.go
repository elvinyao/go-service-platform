package concurrency

import "testing"

func TestNewWorkerPool(t *testing.T) {
	pool := NewWorkerPool(3)
	if pool.workers != 3 {
		t.Fatalf("workers = %d, want 3", pool.workers)
	}
	if pool.queue == nil {
		t.Fatalf("queue is nil")
	}
}
