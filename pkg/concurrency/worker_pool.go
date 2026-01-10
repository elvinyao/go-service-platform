package concurrency

import "sync"

type Task func() error

type WorkerPool struct {
	workers int
	queue   chan Task
	wg      sync.WaitGroup
}

func NewWorkerPool(workers int) *WorkerPool {
	// Implementation
	return &WorkerPool{
		workers: workers,
		queue:   make(chan Task),
		wg:      sync.WaitGroup{},
	}
}
