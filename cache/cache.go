package cache

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "toomorephotos:"

// Cache defines the interface for cache operations.
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) (bool, error)
	Set(ctx context.Context, key string, val interface{}, ttl time.Duration) error
}

// MemoryCache is an in-memory cache implementation.
type MemoryCache struct {
	mu    sync.RWMutex
	store map[string]memoryEntry
}

type memoryEntry struct {
	data     []byte
	expiresAt time.Time
}

// NewMemoryCache creates a new in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{store: make(map[string]memoryEntry)}
}

func (m *MemoryCache) Get(ctx context.Context, key string, dest interface{}) (bool, error) {
	fullKey := keyPrefix + key
	m.mu.RLock()
	ent, ok := m.store[fullKey]
	m.mu.RUnlock()
	if !ok || time.Now().After(ent.expiresAt) {
		return false, nil
	}
	if err := json.Unmarshal(ent.data, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (m *MemoryCache) Set(ctx context.Context, key string, val interface{}, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	fullKey := keyPrefix + key
	m.mu.Lock()
	m.store[fullKey] = memoryEntry{data: data, expiresAt: time.Now().Add(ttl)}
	m.mu.Unlock()
	return nil
}

// RedisCache is a Redis-backed cache implementation.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache creates a new Redis cache from REDIS_URL.
func NewRedisCache(addr string) (*RedisCache, error) {
	opt, err := redis.ParseURL(addr)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	return &RedisCache{client: client}, nil
}

func (r *RedisCache) Get(ctx context.Context, key string, dest interface{}) (bool, error) {
	fullKey := keyPrefix + key
	val, err := r.client.Get(ctx, fullKey).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(val, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (r *RedisCache) Set(ctx context.Context, key string, val interface{}, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	fullKey := keyPrefix + key
	return r.client.Set(ctx, fullKey, data, ttl).Err()
}

// New returns a Cache implementation based on REDIS_URL.
// If REDIS_URL is set, returns RedisCache; otherwise returns MemoryCache.
func New() Cache {
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		log.Println("Cache: using in-memory (REDIS_URL not set)")
		return NewMemoryCache()
	}
	rc, err := NewRedisCache(addr)
	if err != nil {
		log.Printf("Cache: Redis connect failed (%v), falling back to memory", err)
		return NewMemoryCache()
	}
	log.Println("Cache: using Redis")
	return rc
}
