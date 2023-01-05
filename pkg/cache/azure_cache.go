/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"
)

// AzureCacheReadType defines the read type for cache data
type AzureCacheReadType int

const (
	// CacheReadTypeDefault returns data from cache if cache entry not expired
	// if cache entry expired, then it will refetch the data using getter
	// save the entry in cache and then return
	CacheReadTypeDefault AzureCacheReadType = iota
	// CacheReadTypeUnsafe returns data from cache even if the cache entry is
	// active/expired. If entry doesn't exist in cache, then data is fetched
	// using getter, saved in cache and returned
	CacheReadTypeUnsafe
	// CacheReadTypeForceRefresh force refreshes the cache even if the cache entry
	// is not expired
	CacheReadTypeForceRefresh
)

// GetFunc defines a getter function for timedCache.
type GetFunc func(key string) (interface{}, error)

// azureCacheEntry is the internal structure stores inside TTLStore.
type azureCacheEntry struct {
	key  string
	data interface{}

	// The lock to ensure not updating same entry simultaneously.
	lock sync.Mutex
	// time when entry was fetched and created
	createdOn time.Time
}

// cacheKeyFunc defines the key function required in TTLStore.
func cacheKeyFunc(obj interface{}) (string, error) {
	return obj.(*azureCacheEntry).key, nil
}

// TimedCache is a cache with TTL.
type TimedCache struct {
	store  cache.Store
	lock   sync.Mutex
	getter GetFunc
	ttl    time.Duration
}

// NewTimedcache creates a new TimedCache.
func NewTimedcache(ttl time.Duration, getter GetFunc) (*TimedCache, error) {
	if getter == nil {
		return nil, fmt.Errorf("getter is not provided")
	}

	return &TimedCache{
		getter: getter,
		// switch to using NewStore instead of NewTTLStore so that we can
		// reuse entries for calls that are fine with reading expired/stalled data.
		// with NewTTLStore, entries are not returned if they have already expired.
		store: cache.NewStore(cacheKeyFunc),
		ttl:   ttl,
	}, nil
}

// getInternal returns AzureCacheEntry by key. If the key is not cached yet,
// it returns a AzureCacheEntry with nil data.
func (t *TimedCache) getInternal(key string) (*azureCacheEntry, error) {
	entry, exists, err := t.store.GetByKey(key)
	if err != nil {
		return nil, err
	}
	// if entry exists, return the entry
	if exists {
		return entry.(*azureCacheEntry), nil
	}

	// lock here to ensure if entry doesn't exist, we add a new entry
	// avoiding overwrites
	t.lock.Lock()
	defer t.lock.Unlock()

	// Another goroutine might have written the same key.
	entry, exists, err = t.store.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if exists {
		return entry.(*azureCacheEntry), nil
	}

	// Still not found, add new entry with nil data.
	// Note the data will be filled later by getter.
	newEntry := &azureCacheEntry{
		key:  key,
		data: nil,
	}
	_ = t.store.Add(newEntry)
	return newEntry, nil
}

// Get returns the requested item by key.
func (t *TimedCache) Get(key string, crt AzureCacheReadType) (interface{}, error) {
	entry, err := t.getInternal(key)
	if err != nil {
		return nil, err
	}

	entry.lock.Lock()
	defer entry.lock.Unlock()

	// entry exists and if cache is not force refreshed
	if entry.data != nil && crt != CacheReadTypeForceRefresh {
		// allow unsafe read, so return data even if expired
		if crt == CacheReadTypeUnsafe {
			return entry.data, nil
		}
		// if cached data is not expired, return cached data
		if crt == CacheReadTypeDefault && time.Since(entry.createdOn) < t.ttl {
			return entry.data, nil
		}
	}
	// Data is not cached yet, cache data is expired or requested force refresh
	// cache it by getter. entry is locked before getting to ensure concurrent
	// gets don't result in multiple ARM calls.
	data, err := t.getter(key)
	if err != nil {
		return nil, err
	}

	// set the data in cache and also set the last update time
	// to now as the data was recently fetched
	entry.data = data
	entry.createdOn = time.Now().UTC()

	return entry.data, nil
}

// TryGet returns the entry if it exists and nil otherwise.
func (t *TimedCache) TryGet(key string) (interface{}, error) {
	entry, _, err := t.store.GetByKey(key)
	if entry == nil || err != nil {
		return nil, err
	}

	return entry.(*azureCacheEntry).data, nil
}

// Delete removes an item from the cache.
func (t *TimedCache) Delete(key string) error {
	return t.store.Delete(&azureCacheEntry{
		key: key,
	})
}

// Set sets the data cache for the key.
// It is only used for testing.
func (t *TimedCache) Set(key string, data interface{}) {
	_ = t.store.Add(&azureCacheEntry{
		key:       key,
		data:      data,
		createdOn: time.Now().UTC(),
	})
}

// Update updates the data cache for the key.
func (t *TimedCache) Update(key string, data interface{}) {
	_ = t.store.Update(&azureCacheEntry{
		key:       key,
		data:      data,
		createdOn: time.Now().UTC(),
	})
}

// ListKeys returns a list of all of the keys of objects currently in the cache.
func (t *TimedCache) ListKeys() []string {
	return t.store.ListKeys()
}

// Range calls f sequentially for each value present in the cache. If f returns
// false, range stops the iteration.
func (t *TimedCache) Range(f func(key string, value interface{}) bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	for _, entry := range t.store.List() {
		if entry != nil {
			cached := entry.(*azureCacheEntry)
			if cached.data != nil {
				if !f(cached.key, cached.data) {
					break
				}
			}
		}
	}
}
