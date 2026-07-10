# Bounded Concurrency

`pkg/concurrency` provides a small batch-oriented worker pool for independent tasks. It limits active work, waits for scheduled tasks, and returns all task failures in input order.

## Basic Usage

```go
pool, err := concurrency.NewWorkerPool(4)
if err != nil {
    return err
}

err = pool.Run(ctx,
    func() error { return indexBatch(ctx, "customers") },
    func() error { return indexBatch(ctx, "orders") },
    func() error { return indexBatch(ctx, "invoices") },
)
```

`NewWorkerPool` rejects non-positive worker counts. `Run` rejects a nil context or nil task before starting work.

## Execution Contract

- At most the configured number of tasks run concurrently.
- Every scheduled task runs at most once.
- `Run` waits for all scheduled tasks before returning.
- Task errors are wrapped with their input indexes and joined in input order.
- Context cancellation stops scheduling additional tasks and is included in the returned error.
- Cancellation cannot interrupt a task that is already running. Capture the context inside each task and pass it to blocking operations.
- A pool has no per-run mutable state and can be reused for later batches.

Use `errors.Is` or `errors.As` to inspect joined failures.

## Appropriate Uses

The helper fits bounded fan-out such as refreshing independent providers, processing a finite batch, or running several maintenance checks. It is not a persistent queue, retry system, scheduler, or replacement for input-level backpressure.
