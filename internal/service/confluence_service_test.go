package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"project/internal/dataaccess"

	"github.com/stretchr/testify/assert"
)

type stubDataAccessor struct {
	mu   sync.Mutex
	data map[string]interface{}
}

var _ dataaccess.DataAccessor = (*stubDataAccessor)(nil)

func newStubDataAccessor() *stubDataAccessor {
	return &stubDataAccessor{
		data: make(map[string]interface{}),
	}
}

func (s *stubDataAccessor) GetData(key string) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

func (s *stubDataAccessor) SetData(key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	return nil
}

func runWithTimeout(t *testing.T, timeout time.Duration, fn func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatalf("operation timed out after %v", timeout)
		return nil
	}
}

func TestConfluenceService_Start_DoesNotDeadlock(t *testing.T) {
	service := NewConfluenceService("ConfluenceServiceA", "A", newStubDataAccessor(), "1.0.0")
	ctx := context.Background()

	err := runWithTimeout(t, 1*time.Second, func() error {
		return service.Start(ctx)
	})

	assert.NoError(t, err)
	assert.True(t, service.IsRunning(ctx))
}

func TestConfluenceService_ConcurrentStart_NoHang(t *testing.T) {
	service := NewConfluenceService("ConfluenceServiceA", "A", newStubDataAccessor(), "1.0.0")
	ctx := context.Background()

	const workers = 10
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- service.Start(ctx)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Start calls timed out; possible deadlock")
	}

	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
	assert.True(t, service.IsRunning(ctx))
}

func TestConfluenceService_StartThenStop_NoHang(t *testing.T) {
	service := NewConfluenceService("ConfluenceServiceA", "A", newStubDataAccessor(), "1.0.0")
	ctx := context.Background()

	startErr := runWithTimeout(t, 1*time.Second, func() error {
		return service.Start(ctx)
	})
	assert.NoError(t, startErr)
	assert.True(t, service.IsRunning(ctx))

	stopErr := runWithTimeout(t, 1*time.Second, func() error {
		return service.Stop(ctx)
	})
	assert.NoError(t, stopErr)
	assert.False(t, service.IsRunning(ctx))
}
