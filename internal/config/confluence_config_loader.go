package config

import (
	"time"

	"github.com/patrickmn/go-cache"
)

type ConfluenceConfigLoader struct {
	client          interface{} // Confluence客户端
	pageID          string
	refreshInterval time.Duration
	cache           *cache.Cache
}

// 实现方法
