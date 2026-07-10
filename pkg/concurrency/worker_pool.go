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

// Run executes each task at most once and waits for all scheduled tasks.
// Cancellation stops scheduling new work but cannot interrupt a Task that is
// already running. Returned task errors are ordered by their input position.
func (p *WorkerPool) Run(ctx context.Context, tasks ...Task) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	for index, task := range tasks {
		if task == nil {
			return fmt.Errorf("task %d is nil", index)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}

	type workItem struct {
		index int
		task  Task
	}

	queue := make(chan workItem)
	taskErrors := make([]error, len(tasks))
	var workers sync.WaitGroup
	workers.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-queue:
					if !ok {
						return
					}
					taskErrors[item.index] = item.task()
				}
			}
		}()
	}

schedule:
	for index, task := range tasks {
		select {
		case <-ctx.Done():
			break schedule
		case queue <- workItem{index: index, task: task}:
		}
	}
	close(queue)
	workers.Wait()

	result := make([]error, 0)
	for index, taskErr := range taskErrors {
		if taskErr != nil {
			result = append(result, fmt.Errorf("task %d: %w", index, taskErr))
		}
	}
	if err := ctx.Err(); err != nil {
		result = append(result, err)
	}
	return errors.Join(result...)
}
