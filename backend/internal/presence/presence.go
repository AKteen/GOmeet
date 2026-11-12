package presence

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const ttlSeconds = 90

// ErrFull is returned when a join would exceed the room's participant cap.
var ErrFull = errors.New("room is full")

// Service tracks who is in a room. Members live in a Redis sorted set scored by last heartbeat.
type Service struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Service {
	return &Service{rdb: rdb}
}

func membersKey(slug string) string {
	return "room:" + slug + ":members"
}

// Prune removes members whose score is older than 90 seconds and returns their user ids.
func (s *Service) Prune(ctx context.Context, slug string) ([]string, error) {
	key := membersKey(slug)
	cutoff := strconv.FormatInt(time.Now().Unix()-ttlSeconds, 10)
	stale, err := s.rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{Min: "-inf", Max: cutoff}).Result()
	if err != nil {
		return nil, err
	}
	if len(stale) > 0 {
		if err := s.rdb.ZRemRangeByScore(ctx, key, "-inf", cutoff).Err(); err != nil {
			return nil, err
		}
	}
	return stale, nil
}

// Join records the user and returns the current member count. Over-capacity joins are removed.
func (s *Service) Join(ctx context.Context, slug string, userID uuid.UUID, maxParticipants int) (int, error) {
	if _, err := s.Prune(ctx, slug); err != nil {
		return 0, err
	}
	key := membersKey(slug)
	member := userID.String()
	if err := s.rdb.ZAdd(ctx, key, redis.Z{Score: float64(time.Now().Unix()), Member: member}).Err(); err != nil {
		return 0, err
	}
	count, err := s.rdb.ZCard(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if int(count) > maxParticipants {
		if err := s.rdb.ZRem(ctx, key, member).Err(); err != nil {
			return 0, err
		}
		return 0, ErrFull
	}
	return int(count), nil
}

// Heartbeat refreshes the user's presence score.
func (s *Service) Heartbeat(ctx context.Context, slug string, userID uuid.UUID) error {
	return s.rdb.ZAdd(ctx, membersKey(slug), redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: userID.String(),
	}).Err()
}

// Leave removes the user and returns how many members remain.
func (s *Service) Leave(ctx context.Context, slug string, userID uuid.UUID) (int, error) {
	key := membersKey(slug)
	if err := s.rdb.ZRem(ctx, key, userID.String()).Err(); err != nil {
		return 0, err
	}
	count, err := s.rdb.ZCard(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}
