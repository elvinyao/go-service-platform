package ruleengine

import (
	"fmt"
	"time"
)

const (
	defaultDispatchWorkers   = 4
	defaultDispatchQueueSize = 64
	defaultDedupTTL          = "10m"
	defaultDedupMaxEntries   = 10000
)

// WithDefaults fills unset dispatch settings. Deduplication stays off unless
// it is explicitly enabled.
func (c DispatchConfig) WithDefaults() DispatchConfig {
	if c.Workers == 0 {
		c.Workers = defaultDispatchWorkers
	}
	if c.QueueSize == 0 {
		c.QueueSize = defaultDispatchQueueSize
	}
	if c.QueueFull == "" {
		c.QueueFull = QueueFullBlock
	}
	if c.Dedup.TTL == "" {
		c.Dedup.TTL = defaultDedupTTL
	}
	if c.Dedup.MaxEntries == 0 {
		c.Dedup.MaxEntries = defaultDedupMaxEntries
	}
	return c
}

// Validate checks dispatch settings after defaults are applied.
func (c DispatchConfig) Validate() error {
	c = c.WithDefaults()
	if c.Workers < 1 {
		return fmt.Errorf("dispatch.workers must be at least 1")
	}
	if c.QueueSize < 1 {
		return fmt.Errorf("dispatch.queue_size must be at least 1")
	}
	if c.QueueFull != QueueFullBlock && c.QueueFull != QueueFullDrop {
		return fmt.Errorf("dispatch.queue_full %q is invalid", c.QueueFull)
	}
	if c.OrderingKey != "" && !validConditionField(c.OrderingKey) {
		return fmt.Errorf("dispatch.ordering_key %q is invalid", c.OrderingKey)
	}
	if _, err := c.DedupTTL(); err != nil {
		return err
	}
	if c.Dedup.MaxEntries < 1 {
		return fmt.Errorf("dispatch.dedup.max_entries must be at least 1")
	}
	return nil
}

// DedupTTL returns the parsed deduplication window.
func (c DispatchConfig) DedupTTL() (time.Duration, error) {
	ttl, err := parseOptionalDuration("dispatch.dedup.ttl", c.WithDefaults().Dedup.TTL)
	if err != nil {
		return 0, err
	}
	if ttl == 0 {
		return 0, fmt.Errorf("dispatch.dedup.ttl must be greater than zero")
	}
	return ttl, nil
}
