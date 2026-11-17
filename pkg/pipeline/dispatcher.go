package pipeline

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// Errors returned by Dispatcher.Submit when an event is not accepted.
var (
	// ErrDuplicateEvent means an event with the same ID was accepted recently.
	ErrDuplicateEvent = errors.New("duplicate event")
	// ErrQueueFull means the event was dropped because its queue was full.
	ErrQueueFull = errors.New("dispatch queue is full")
	// ErrDispatcherStopped means the dispatcher no longer accepts events.
	ErrDispatcherStopped = errors.New("dispatcher is stopped")
)

// Handler processes one accepted event. It runs on a dispatcher worker with a
// context that keeps the submitter's values but not its cancellation.
type Handler func(ctx context.Context, message ruleengine.Message)

// DispatchStats is a point-in-time view of dispatcher activity.
type DispatchStats struct {
	Workers    int   `json:"workers"`
	Queued     int   `json:"queued"`
	Accepted   int64 `json:"accepted"`
	Processed  int64 `json:"processed"`
	Duplicates int64 `json:"duplicates"`
	Dropped    int64 `json:"dropped"`
	Panics     int64 `json:"panics"`
}

type dispatchItem struct {
	ctx     context.Context
	message ruleengine.Message
}

// Dispatcher processes events concurrently on a fixed set of workers.
//
// It bounds memory with per-worker queues, keeps events that share an
// ordering key in arrival order, and optionally drops redelivered events by
// Message.ID. A Dispatcher is single-use: after Stop it cannot be restarted.
type Dispatcher struct {
	config  ruleengine.DispatchConfig
	handler Handler
	queues  []chan dispatchItem
	dedup   *dedupStore

	mu         sync.RWMutex
	stopped    bool
	stopping   chan struct{}
	submitters sync.WaitGroup
	workers    sync.WaitGroup
	closeOnce  sync.Once

	next       atomic.Uint64
	accepted   atomic.Int64
	processed  atomic.Int64
	duplicates atomic.Int64
	dropped    atomic.Int64
	panics     atomic.Int64
}

// NewDispatcher validates config, applies defaults, and starts the workers.
func NewDispatcher(config ruleengine.DispatchConfig, handler Handler) (*Dispatcher, error) {
	if handler == nil {
		return nil, fmt.Errorf("dispatch handler is nil")
	}
	config = config.WithDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	d := &Dispatcher{
		config:   config,
		handler:  handler,
		queues:   make([]chan dispatchItem, config.Workers),
		stopping: make(chan struct{}),
	}
	if config.Dedup.Enabled {
		ttl, _ := config.DedupTTL() // validated above
		d.dedup = newDedupStore(ttl, config.Dedup.MaxEntries, time.Now)
	}
	for i := range d.queues {
		d.queues[i] = make(chan dispatchItem, config.QueueSize)
		d.workers.Add(1)
		go d.work(d.queues[i])
	}
	return d, nil
}

// Submit hands an event to a worker. It returns nil once the event is queued,
// ErrDuplicateEvent or ErrQueueFull when the event is intentionally not
// processed, ErrDispatcherStopped after Stop, or ctx.Err() if ctx ends while
// waiting for queue space.
func (d *Dispatcher) Submit(ctx context.Context, message ruleengine.Message) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.RLock()
	if d.stopped {
		d.mu.RUnlock()
		return ErrDispatcherStopped
	}
	d.submitters.Add(1)
	d.mu.RUnlock()
	defer d.submitters.Done()

	if d.dedup != nil && message.ID != "" && !d.dedup.claim(message.ID) {
		d.duplicates.Add(1)
		return ErrDuplicateEvent
	}

	item := dispatchItem{ctx: context.WithoutCancel(ctx), message: message}
	queue := d.queues[d.queueIndex(message)]
	err := d.enqueue(ctx, queue, item)
	if err != nil {
		// The event was not processed, so a redelivery must be allowed.
		if d.dedup != nil && message.ID != "" {
			d.dedup.forget(message.ID)
		}
		if errors.Is(err, ErrQueueFull) {
			d.dropped.Add(1)
		}
		return err
	}
	d.accepted.Add(1)
	return nil
}

func (d *Dispatcher) enqueue(ctx context.Context, queue chan dispatchItem, item dispatchItem) error {
	if d.config.QueueFull == ruleengine.QueueFullDrop {
		select {
		case queue <- item:
			return nil
		default:
			return ErrQueueFull
		}
	}
	select {
	case queue <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.stopping:
		return ErrDispatcherStopped
	}
}

// queueIndex pins events that share an ordering key to one worker and spreads
// the rest round-robin.
func (d *Dispatcher) queueIndex(message ruleengine.Message) int {
	if d.config.OrderingKey != "" {
		if value, ok := message.Field(d.config.OrderingKey); ok {
			if key := fmt.Sprintf("%v", value); key != "" {
				hash := fnv.New32a()
				_, _ = hash.Write([]byte(key))
				return int(hash.Sum32() % uint32(len(d.queues)))
			}
		}
	}
	return int(d.next.Add(1) % uint64(len(d.queues)))
}

func (d *Dispatcher) work(queue chan dispatchItem) {
	defer d.workers.Done()
	for item := range queue {
		d.handle(item)
	}
}

func (d *Dispatcher) handle(item dispatchItem) {
	defer func() {
		if recovered := recover(); recovered != nil {
			d.panics.Add(1)
		}
		d.processed.Add(1)
	}()
	d.handler(item.ctx, item.message)
}

// Stop rejects new events, lets workers finish everything already queued, and
// waits for them until ctx ends. It is safe to call more than once.
func (d *Dispatcher) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	if !d.stopped {
		d.stopped = true
		close(d.stopping)
	}
	d.mu.Unlock()

	// Submitters blocked on a full queue observe d.stopping and return, so the
	// queues can be closed without racing a send.
	d.submitters.Wait()
	d.closeOnce.Do(func() {
		for _, queue := range d.queues {
			close(queue)
		}
	})

	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for dispatcher workers: %w", ctx.Err())
	}
}

// Stats returns current counters and queue depth.
func (d *Dispatcher) Stats() DispatchStats {
	queued := 0
	for _, queue := range d.queues {
		queued += len(queue)
	}
	return DispatchStats{
		Workers:    len(d.queues),
		Queued:     queued,
		Accepted:   d.accepted.Load(),
		Processed:  d.processed.Load(),
		Duplicates: d.duplicates.Load(),
		Dropped:    d.dropped.Load(),
		Panics:     d.panics.Load(),
	}
}

type dedupEntry struct {
	id      string
	expires time.Time
}

// dedupStore remembers recently accepted event IDs for a bounded time and size.
type dedupStore struct {
	ttl        time.Duration
	maxEntries int
	now        func() time.Time

	mu      sync.Mutex
	expires map[string]time.Time
	order   []dedupEntry
}

func newDedupStore(ttl time.Duration, maxEntries int, now func() time.Time) *dedupStore {
	return &dedupStore{
		ttl:        ttl,
		maxEntries: maxEntries,
		now:        now,
		expires:    make(map[string]time.Time),
	}
}

// claim records id and reports true when it was not seen within the TTL.
func (s *dedupStore) claim(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.evictLocked(now)
	if expires, seen := s.expires[id]; seen && now.Before(expires) {
		return false
	}
	expires := now.Add(s.ttl)
	s.expires[id] = expires
	s.order = append(s.order, dedupEntry{id: id, expires: expires})
	for len(s.expires) > s.maxEntries {
		s.dropOldestLocked()
	}
	return true
}

func (s *dedupStore) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.expires, id)
}

func (s *dedupStore) evictLocked(now time.Time) {
	for len(s.order) > 0 && !now.Before(s.order[0].expires) {
		s.dropOldestLocked()
	}
}

// dropOldestLocked removes the oldest order entry, and its ID unless the ID
// was forgotten and claimed again with a newer expiry.
func (s *dedupStore) dropOldestLocked() {
	oldest := s.order[0]
	s.order = s.order[1:]
	if expires, ok := s.expires[oldest.id]; ok && expires.Equal(oldest.expires) {
		delete(s.expires, oldest.id)
	}
}
