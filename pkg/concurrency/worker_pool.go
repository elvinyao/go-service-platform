package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Task is one unit of work executed by a WorkerPool.
type Task func() error

// WorkerPool limits concurrent execution of a batch of independent tasks.
// A pool is stateless after construction and can safely run multiple batches.
type WorkerPool struct {
	workers int
}

// NewWorkerPool creates a pool with a fixed positive concurrency limit.
func NewWorkerPool(workers int) (*WorkerPool, error) {
	if workers <= 0 {
		return nil, fmt.Errorf("worker count must be greater than zero")
	}
	return &WorkerPool{workers: workers}, nil
}

type batchState uint8

const (
	batchStatePreflight batchState = iota
	batchStateDispatching
	batchStateCancelled
	batchStateDraining
	batchStateCompleted
)

type workItem struct {
	index int
	task  Task
}

type batchExecution struct {
	state      batchState
	workers    int
	tasks      []Task
	queue      chan workItem
	taskErrors []error
	wg         sync.WaitGroup
}

// Run executes each task at most once and waits for all scheduled tasks.
// Cancellation stops scheduling new work but cannot interrupt a Task that is
// already running. Returned task errors are ordered by their input position.
func (p *WorkerPool) Run(ctx context.Context, tasks ...Task) error {
	batch := &batchExecution{
		state:   batchStatePreflight,
		workers: p.workers,
		tasks:   tasks,
	}
	return batch.run(ctx)
}

func (b *batchExecution) run(ctx context.Context) error {
	if err := b.preflight(ctx); err != nil || b.state == batchStateCompleted {
		return err
	}

	b.startWorkers(ctx)
	b.dispatch(ctx)
	b.drain()
	return b.collectErrors(ctx)
}

func (b *batchExecution) preflight(ctx context.Context) error {
	if ctx == nil {
		b.state = batchStateCompleted
		return fmt.Errorf("context is nil")
	}
	for index, task := range b.tasks {
		if task == nil {
			b.state = batchStateCompleted
			return fmt.Errorf("task %d is nil", index)
		}
	}
	if err := ctx.Err(); err != nil {
		b.state = batchStateCompleted
		return err
	}
	if len(b.tasks) == 0 {
		b.state = batchStateCompleted
		return nil
	}

	b.queue = make(chan workItem)
	b.taskErrors = make([]error, len(b.tasks))
	b.state = batchStateDispatching
	return nil
}

func (b *batchExecution) startWorkers(ctx context.Context) {
	b.wg.Add(b.workers)
	for i := 0; i < b.workers; i++ {
		go func() {
			defer b.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-b.queue:
					if !ok {
						return
					}
					b.taskErrors[item.index] = item.task()
				}
			}
		}()
	}
}

func (b *batchExecution) dispatch(ctx context.Context) {
	for index, task := range b.tasks {
		select {
		case <-ctx.Done():
			b.state = batchStateCancelled
			return
		case b.queue <- workItem{index: index, task: task}:
		}
	}
	b.state = batchStateDraining
}

func (b *batchExecution) drain() {
	close(b.queue)
	b.wg.Wait()
	b.state = batchStateCompleted
}

func (b *batchExecution) collectErrors(ctx context.Context) error {
	result := make([]error, 0)
	for index, taskErr := range b.taskErrors {
		if taskErr != nil {
			result = append(result, fmt.Errorf("task %d: %w", index, taskErr))
		}
	}
	if err := ctx.Err(); err != nil {
		result = append(result, err)
	}
	return errors.Join(result...)
}
