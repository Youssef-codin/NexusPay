package redisDb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/redis/go-redis/v9"
)

type Transactions struct {
	client *redis.Client
}

func NewTransactions(client *redis.Client) *Transactions {
	return &Transactions{client: client}
}

func (t *Transactions) Get(ctx context.Context, id string) (*repo.Transaction, error) {
	key := fmt.Sprintf("transactions:%s", id)
	data, err := t.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var tx repo.Transaction
	if err := json.Unmarshal([]byte(data), &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

func (t *Transactions) Set(
	ctx context.Context,
	id string,
	tx *repo.Transaction,
	ttl ...time.Duration,
) error {
	key := fmt.Sprintf("transactions:%s", id)
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		return t.client.Set(ctx, key, data, ttl[0]).Err()
	}
	return t.client.Set(ctx, key, data, 0).Err()
}

func (t *Transactions) Del(ctx context.Context, id string) error {
	key := fmt.Sprintf("transactions:%s", id)
	return t.client.Del(ctx, key).Err()
}
