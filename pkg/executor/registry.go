package executor

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
}

func NewRegistry() *Registry {
	return &Registry{
		executors: make(map[string]Executor),
	}
}

func (r *Registry) Register(executor Executor) error {
	if executor == nil {
		return fmt.Errorf("executor is nil")
	}
	name := executor.Type()
	if name == "" {
		return fmt.Errorf("executor type is empty")
	}

	r.mu.Lock()
	r.executors[name] = executor
	r.mu.Unlock()
	return nil
}

func (r *Registry) Get(name string) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	executor, ok := r.executors[name]
	return executor, ok
}

func (r *Registry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.executors))
	for name := range r.executors {
		types = append(types, name)
	}
	sort.Strings(types)
	return types
}
