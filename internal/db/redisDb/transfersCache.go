package redisDb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/redis/go-redis/v9"
)

type Transfers struct {
	client *redis.Client
}

func NewTransfers(client *redis.Client) *Transfers {
	return &Transfers{client: client}
}

func (t *Transfers) Get(ctx context.Context, id string) (*repo.Transfer, error) {
	key := fmt.Sprintf("transfers:%s", id)
	data, err := t.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var transfer repo.Transfer
	if err := json.Unmarshal([]byte(data), &transfer); err != nil {
		return nil, err
	}
	return &transfer, nil
}

func (t *Transfers) Set(
	ctx context.Context,
	id string,
	transfer *repo.Transfer,
	ttl ...time.Duration,
) error {
	key := fmt.Sprintf("transfers:%s", id)
	data, err := json.Marshal(transfer)
	if err != nil {
		return err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		return t.client.Set(ctx, key, data, ttl[0]).Err()
	}
	return t.client.Set(ctx, key, data, 0).Err()
}

func (t *Transfers) Del(ctx context.Context, id string) error {
	key := fmt.Sprintf("transfers:%s", id)
	return t.client.Del(ctx, key).Err()
}
