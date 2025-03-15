package connections

import "sync"

type ConnectionManager struct {
	connections map[string]interface{}
	mu          sync.RWMutex
}

// 实现方法
