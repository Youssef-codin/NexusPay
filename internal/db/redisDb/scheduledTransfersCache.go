package redisDb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/redis/go-redis/v9"
)

type ScheduledTransfers struct {
	client *redis.Client
}

func NewScheduledTransfers(client *redis.Client) *ScheduledTransfers {
	return &ScheduledTransfers{client: client}
}

func (s *ScheduledTransfers) Get(ctx context.Context, id string) (*repo.ScheduledTransfer, error) {
	key := fmt.Sprintf("scheduled_transfers:%s", id)
	data, err := s.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var st repo.ScheduledTransfer
	if err := json.Unmarshal([]byte(data), &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *ScheduledTransfers) Set(
	ctx context.Context,
	id string,
	st *repo.ScheduledTransfer,
	ttl ...time.Duration,
) error {
	key := fmt.Sprintf("scheduled_transfers:%s", id)
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		return s.client.Set(ctx, key, data, ttl[0]).Err()
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

func (s *ScheduledTransfers) Del(ctx context.Context, id string) error {
	key := fmt.Sprintf("scheduled_transfers:%s", id)
	return s.client.Del(ctx, key).Err()
}
