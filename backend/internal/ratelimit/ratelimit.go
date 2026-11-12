package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLimited is returned when the counter for key is above limit inside the window.
var ErrLimited = errors.New("rate limit exceeded")

// Check increments key and rejects calls past limit. The window starts on the first hit.
func Check(ctx context.Context, rdb *redis.Client, key string, limit int, window time.Duration) error {
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return err
	}
	if count == 1 {
		if err := rdb.Expire(ctx, key, window).Err(); err != nil {
			return err
		}
	}
	if count > int64(limit) {
		return ErrLimited
	}
	return nil
}
