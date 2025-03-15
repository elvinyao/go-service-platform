package dataaccess

import (
	"encoding/json"
	"time"

	"github.com/allegro/bigcache/v3"
)

type CacheDataAccessor struct {
	cache *bigcache.BigCache
}

func NewCacheDataAccessor() *CacheDataAccessor {
	cache, _ := bigcache.NewBigCache(bigcache.DefaultConfig(10 * time.Minute))
	return &CacheDataAccessor{
		cache: cache,
	}
}

func (da *CacheDataAccessor) GetData(key string) (interface{}, error) {
	data, err := da.cache.Get(key)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (da *CacheDataAccessor) SetData(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return da.cache.Set(key, data)
}
