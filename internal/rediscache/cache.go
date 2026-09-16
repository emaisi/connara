package rediscache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client     *redis.Client
	startupErr error
}

func Unavailable(err error) *Cache { return &Cache{startupErr: err} }

func Open(ctx context.Context, address, password string, database int) (*Cache, error) {
	if address == "" {
		return nil, errors.New("Redis address is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:         address,
		Password:     password,
		DB:           database,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})
	cache := &Cache{client: client}
	if err := cache.Ready(ctx); err != nil {
		return cache, err
	}
	return cache, nil
}

func (c *Cache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *Cache) Ready(ctx context.Context) error {
	if err := c.available(); err != nil {
		return err
	}
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

func (c *Cache) PutOAuthState(ctx context.Context, hash string, value []byte, ttl time.Duration) error {
	if err := c.available(); err != nil {
		return err
	}
	return c.client.Set(ctx, "apihub:oauth:state:"+hash, value, ttl).Err()
}

func (c *Cache) TakeOAuthState(ctx context.Context, hash string) ([]byte, error) {
	if err := c.available(); err != nil {
		return nil, err
	}
	value, err := c.client.GetDel(ctx, "apihub:oauth:state:"+hash).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, errors.New("oauth state not found or expired")
	}
	return value, err
}

func (c *Cache) AcquireRefreshLock(ctx context.Context, connectionID string, ttl time.Duration) (func(context.Context) error, bool, error) {
	if err := c.available(); err != nil {
		return nil, false, err
	}
	token, err := randomToken(16)
	if err != nil {
		return nil, false, err
	}
	key := "apihub:auth:refresh-lock:" + connectionID
	acquired, err := c.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !acquired {
		return nil, acquired, err
	}
	unlock := func(unlockContext context.Context) error {
		const script = `
			if redis.call('get', KEYS[1]) == ARGV[1] then
				return redis.call('del', KEYS[1])
			end
			return 0`
		return c.client.Eval(unlockContext, script, []string{key}, token).Err()
	}
	return unlock, true, nil
}

func (c *Cache) Allow(ctx context.Context, scope string, limit int64, window time.Duration) (bool, int64, error) {
	if err := c.available(); err != nil {
		return false, 0, err
	}
	if limit <= 0 {
		return false, 0, nil
	}
	const script = `
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local cutoff = now - tonumber(ARGV[2])
		redis.call('ZREMRANGEBYSCORE', key, 0, cutoff)
		local count = redis.call('ZCARD', key)
		if count >= tonumber(ARGV[3]) then
			redis.call('PEXPIRE', key, ARGV[2])
			return {0, count}
		end
		redis.call('ZADD', key, now, ARGV[4])
		redis.call('PEXPIRE', key, ARGV[2])
		return {1, count + 1}`
	now := time.Now().UTC().UnixMilli()
	member := strconv.FormatInt(now, 10) + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	values, err := c.client.Eval(ctx, script, []string{"apihub:rate:" + scope}, now, window.Milliseconds(), limit, member).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	return values[0] == 1, values[1], nil
}

func (c *Cache) available() error {
	if c != nil && c.client != nil {
		return nil
	}
	if c != nil && c.startupErr != nil {
		return c.startupErr
	}
	return errors.New("Redis is unavailable")
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
