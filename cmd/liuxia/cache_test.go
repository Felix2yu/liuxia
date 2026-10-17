package main

import (
	"sync"
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewCache(ttl)

	if cache == nil {
		t.Fatal("NewCache() 返回 nil")
	}
	if cache.items == nil {
		t.Error("NewCache() items 应已初始化")
	}
	if cache.ttl != ttl {
		t.Errorf("NewCache() ttl = %v, want %v", cache.ttl, ttl)
	}
}

func TestCacheSetAndGet(t *testing.T) {
	cache := NewCache(5 * time.Minute)

	t.Run("设置和获取值", func(t *testing.T) {
		cache.Set("key1", "value1")
		value, exists := cache.Get("key1")

		if !exists {
			t.Error("Get() 应该返回 exists=true")
		}
		if value != "value1" {
			t.Errorf("Get() = %v, want %q", value, "value1")
		}
	})

	t.Run("获取不存在的键", func(t *testing.T) {
		value, exists := cache.Get("nonexistent")
		if exists {
			t.Error("Get() 对于不存在的键应该返回 exists=false")
		}
		if value != nil {
			t.Errorf("Get() 对于不存在的键应该返回 nil, got %v", value)
		}
	})

	t.Run("覆盖已有值", func(t *testing.T) {
		cache.Set("key2", "value1")
		cache.Set("key2", "value2")
		value, _ := cache.Get("key2")

		if value != "value2" {
			t.Errorf("Get() = %v, want %q", value, "value2")
		}
	})
}

func TestCacheDelete(t *testing.T) {
	cache := NewCache(5 * time.Minute)

	cache.Set("key1", "value1")
	cache.Delete("key1")

	_, exists := cache.Get("key1")
	if exists {
		t.Error("Delete() 后 Get() 应该返回 exists=false")
	}

	t.Run("删除不存在的键不报错", func(t *testing.T) {
		cache.Delete("nonexistent")
	})
}

func TestCacheClear(t *testing.T) {
	cache := NewCache(5 * time.Minute)

	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Clear()

	_, exists1 := cache.Get("key1")
	_, exists2 := cache.Get("key2")

	if exists1 || exists2 {
		t.Error("Clear() 后所有键都应该不存在")
	}
}

func TestCacheExpiration(t *testing.T) {
	cache := NewCache(50 * time.Millisecond)

	cache.Set("key1", "value1")

	t.Run("未过期时应该存在", func(t *testing.T) {
		_, exists := cache.Get("key1")
		if !exists {
			t.Error("设置后立即 Get() 应该返回 exists=true")
		}
	})

	t.Run("过期后应该不存在", func(t *testing.T) {
		time.Sleep(100 * time.Millisecond)
		_, exists := cache.Get("key1")
		if exists {
			t.Error("过期后 Get() 应该返回 exists=false")
		}
	})
}

func TestCacheConcurrency(t *testing.T) {
	cache := NewCache(5 * time.Minute)
	var wg sync.WaitGroup

	t.Run("并发读写", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := "key"
				value := i
				cache.Set(key, value)
				cache.Get(key)
			}(i)
		}
		wg.Wait()
	})
}

func TestCacheTypes(t *testing.T) {
	cache := NewCache(5 * time.Minute)

	t.Run("存储字符串", func(t *testing.T) {
		cache.Set("string", "hello")
		value, _ := cache.Get("string")
		if value != "hello" {
			t.Errorf("Get() = %v, want %q", value, "hello")
		}
	})

	t.Run("存储整数", func(t *testing.T) {
		cache.Set("int", 42)
		value, _ := cache.Get("int")
		if value != 42 {
			t.Errorf("Get() = %v, want %d", value, 42)
		}
	})

	t.Run("存储结构体", func(t *testing.T) {
		type TestStruct struct {
			Name  string
			Value int
		}
		ts := TestStruct{Name: "test", Value: 100}
		cache.Set("struct", ts)
		value, _ := cache.Get("struct")
		if result, ok := value.(TestStruct); !ok || result != ts {
			t.Errorf("Get() = %v, want %v", value, ts)
		}
	})
}

func TestCacheCleanup(t *testing.T) {
	// 创建一个短 TTL 的缓存
	cache := NewCache(100 * time.Millisecond)

	// 添加一些条目
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	// 等待条目过期
	time.Sleep(200 * time.Millisecond)

	// 验证条目已过期
	_, exists1 := cache.Get("key1")
	_, exists2 := cache.Get("key2")
	_, exists3 := cache.Get("key3")

	if exists1 || exists2 || exists3 {
		t.Error("过期的条目应该已被清理")
	}
}

func TestCacheCleanupRoutine(t *testing.T) {
	// 创建一个短 TTL 的缓存，cleanup 间隔也短
	cache := NewCache(50 * time.Millisecond)

	// 添加一些条目
	for i := 0; i < 10; i++ {
		cache.Set(string(rune('a'+i)), i)
	}

	// 等待足够长的时间让 cleanup 运行
	time.Sleep(200 * time.Millisecond)

	// 添加新条目
	cache.Set("new", "value")

	// 验证新条目存在
	value, exists := cache.Get("new")
	if !exists || value != "value" {
		t.Error("新条目应该存在")
	}
}
