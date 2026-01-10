package config

import (
	"time"

	"github.com/patrickmn/go-cache"
)

type ConfluenceConfigLoader struct {
	client          interface{} // Confluence client
	pageID          string
	refreshInterval time.Duration
	cache           *cache.Cache
}

// Implementation methods
