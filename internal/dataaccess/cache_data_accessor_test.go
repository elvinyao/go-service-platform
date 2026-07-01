package dataaccess

import (
	"context"
	"testing"
)

func TestCacheDataAccessorSetAndGetData(t *testing.T) {
	accessor := NewCacheDataAccessor(context.Background())
	value := map[string]interface{}{"name": "test", "count": float64(3)}

	if err := accessor.SetData("key", value); err != nil {
		t.Fatalf("set data: %v", err)
	}

	got, err := accessor.GetData("key")
	if err != nil {
		t.Fatalf("get data: %v", err)
	}
	asMap, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got type = %T, want map", got)
	}
	if asMap["name"] != "test" || asMap["count"] != float64(3) {
		t.Fatalf("got = %+v", asMap)
	}
}

func TestNewCacheDataAccessorAcceptsNilContext(t *testing.T) {
	if accessor := NewCacheDataAccessor(nil); accessor == nil {
		t.Fatalf("accessor is nil")
	}
}

func TestCacheDataAccessorSetDataReturnsMarshalError(t *testing.T) {
	accessor := NewCacheDataAccessor(context.Background())

	err := accessor.SetData("bad", func() {})
	if err == nil {
		t.Fatalf("expected marshal error")
	}
}

func TestCacheDataAccessorGetDataReturnsCacheError(t *testing.T) {
	accessor := NewCacheDataAccessor(context.Background())

	_, err := accessor.GetData("missing")
	if err == nil {
		t.Fatalf("expected missing key error")
	}
}

func TestCacheDataAccessorGetDataReturnsDecodeError(t *testing.T) {
	accessor := NewCacheDataAccessor(context.Background())
	if err := accessor.cache.Set("bad", []byte("{")); err != nil {
		t.Fatalf("set raw data: %v", err)
	}

	_, err := accessor.GetData("bad")
	if err == nil {
		t.Fatalf("expected decode error")
	}
}
