package redisDb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/redis/go-redis/v9"
)

type Wallets struct {
	client *redis.Client
}

func NewWallets(client *redis.Client) *Wallets {
	return &Wallets{client: client}
}

func (w *Wallets) Get(ctx context.Context, id string) (*repo.Wallet, error) {
	key := fmt.Sprintf("wallets:%s", id)
	data, err := w.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var wallet repo.Wallet
	if err := json.Unmarshal([]byte(data), &wallet); err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (w *Wallets) Set(
	ctx context.Context,
	id string,
	wallet *repo.Wallet,
	ttl ...time.Duration,
) error {
	key := fmt.Sprintf("wallets:%s", id)
	data, err := json.Marshal(wallet)
	if err != nil {
		return err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		return w.client.Set(ctx, key, data, ttl[0]).Err()
	}
	return w.client.Set(ctx, key, data, 0).Err()
}

func (w *Wallets) Del(ctx context.Context, id string) error {
	key := fmt.Sprintf("wallets:%s", id)
	return w.client.Del(ctx, key).Err()
}
