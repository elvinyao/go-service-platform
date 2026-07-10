package executor

import (
	"fmt"
	"reflect"
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
	if isNilExecutor(executor) {
		return fmt.Errorf("executor is nil")
	}
	name := executor.Type()
	if name == "" {
		return fmt.Errorf("executor type is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[name]; exists {
		return fmt.Errorf("executor %s is registered more than once", name)
	}
	r.executors[name] = executor
	return nil
}

func isNilExecutor(executor Executor) bool {
	if executor == nil {
		return true
	}
	value := reflect.ValueOf(executor)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
