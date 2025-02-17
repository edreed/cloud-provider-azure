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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	testKey      = "key1"
	fakeCacheTTL = 2 * time.Second
)

type fakeDataObj struct{ Data string }

type fakeDataSource struct {
	called int
	data   map[string]*fakeDataObj
	lock   sync.Mutex
}

func (fake *fakeDataSource) get(key string) (interface{}, error) {
	fake.lock.Lock()
	defer fake.lock.Unlock()

	fake.called = fake.called + 1
	if v, ok := fake.data[key]; ok {
		return v, nil
	}

	return nil, nil
}

func (fake *fakeDataSource) set(data map[string]*fakeDataObj) {
	fake.lock.Lock()
	defer fake.lock.Unlock()

	fake.data = data
	fake.called = 0
}

func newFakeCache(t *testing.T) (*fakeDataSource, *TimedCache) {
	dataSource := &fakeDataSource{
		data: make(map[string]*fakeDataObj),
	}
	getter := dataSource.get
	cache, err := NewTimedcache(fakeCacheTTL, getter)
	assert.NoError(t, err)
	return dataSource, cache
}

func TestCacheGet(t *testing.T) {
	val := &fakeDataObj{}
	cases := []struct {
		name     string
		data     map[string]*fakeDataObj
		key      string
		expected interface{}
	}{
		{
			name:     "cache should return nil for empty data source",
			key:      "key1",
			expected: nil,
		},
		{
			name:     "cache should return nil for non exist key",
			data:     map[string]*fakeDataObj{"key2": val},
			key:      "key1",
			expected: nil,
		},
		{
			name:     "cache should return data for existing key",
			data:     map[string]*fakeDataObj{"key1": val},
			key:      "key1",
			expected: val,
		},
	}

	for _, c := range cases {
		dataSource, cache := newFakeCache(t)
		dataSource.set(c.data)
		val, err := cache.GetWithDeepCopy(c.key, CacheReadTypeDefault)
		assert.NoError(t, err, c.name)
		assert.Equal(t, c.expected, val, c.name)
	}
}

func TestCacheGetError(t *testing.T) {
	getError := fmt.Errorf("getError")
	getter := func(key string) (interface{}, error) {
		return nil, getError
	}
	cache, err := NewTimedcache(fakeCacheTTL, getter)
	assert.NoError(t, err)

	val, err := cache.GetWithDeepCopy("key", CacheReadTypeDefault)
	assert.Error(t, err)
	assert.Equal(t, getError, err)
	assert.Nil(t, val)
}

func TestCacheGetWithDeepCopy(t *testing.T) {
	changed, unchanged := "changed", "unchanged"
	valFake := &fakeDataObj{unchanged}
	cases := []struct {
		name     string
		data     map[string]*fakeDataObj
		key      string
		expected interface{}
	}{
		{
			name:     "cache should return data for existing key",
			data:     map[string]*fakeDataObj{"key1": valFake},
			key:      "key1",
			expected: unchanged,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dataSource, cache := newFakeCache(t)
			dataSource.set(c.data)
			cache.Set(c.key, valFake)
			val, err := cache.GetWithDeepCopy(c.key, CacheReadTypeDefault)
			assert.NoError(t, err)
			assert.Equal(t, c.expected, val.(*fakeDataObj).Data)

			// Change the value
			valFake.Data = changed
			cache.Set(c.key, valFake)
			assert.Equal(t, c.expected, val.(*fakeDataObj).Data)
		})
	}
}

func TestCacheDelete(t *testing.T) {
	val := &fakeDataObj{}
	data := map[string]*fakeDataObj{
		testKey: val,
	}
	dataSource, cache := newFakeCache(t)
	dataSource.set(data)

	v, err := cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, val, v, "cache should get correct data")

	dataSource.set(nil)
	_ = cache.Delete(testKey)
	v, err = cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, nil, v, "cache should get nil after data is removed")
}

func TestCacheExpired(t *testing.T) {
	val := &fakeDataObj{}
	data := map[string]*fakeDataObj{
		testKey: val,
	}
	dataSource, cache := newFakeCache(t)
	dataSource.set(data)

	v, err := cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, val, v, "cache should get correct data")

	time.Sleep(fakeCacheTTL)
	v, err = cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, 2, dataSource.called)
	assert.Equal(t, val, v, "cache should get correct data even after expired")
}

func TestCacheAllowUnsafeRead(t *testing.T) {
	val := &fakeDataObj{}
	data := map[string]*fakeDataObj{
		testKey: val,
	}
	dataSource, cache := newFakeCache(t)
	dataSource.set(data)

	v, err := cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, val, v, "cache should get correct data")

	time.Sleep(fakeCacheTTL)
	v, err = cache.GetWithDeepCopy(testKey, CacheReadTypeUnsafe)
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, val, v, "cache should return expired as allow unsafe read is allowed")
}

func TestCacheNoConcurrentGet(t *testing.T) {
	val := &fakeDataObj{}
	data := map[string]*fakeDataObj{
		testKey: val,
	}
	dataSource, cache := newFakeCache(t)
	dataSource.set(data)

	time.Sleep(fakeCacheTTL)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
		}()
	}
	v, err := cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	wg.Wait()
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, val, v, "cache should get correct data")
}

func TestCacheForceRefresh(t *testing.T) {
	val := &fakeDataObj{}
	data := map[string]*fakeDataObj{
		testKey: val,
	}
	dataSource, cache := newFakeCache(t)
	dataSource.set(data)

	v, err := cache.GetWithDeepCopy(testKey, CacheReadTypeDefault)
	assert.NoError(t, err)
	assert.Equal(t, 1, dataSource.called)
	assert.Equal(t, val, v, "cache should get correct data")

	v, err = cache.GetWithDeepCopy(testKey, CacheReadTypeForceRefresh)
	assert.NoError(t, err)
	assert.Equal(t, 2, dataSource.called)
	assert.Equal(t, val, v, "should refetch unexpired data as forced refresh")
}
