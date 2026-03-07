package redisDb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/redis/go-redis/v9"
)

type Users struct {
	client *redis.Client
}

func NewUsers(client *redis.Client) *Users {
	return &Users{client: client}
}

func (u *Users) Get(ctx context.Context, id string) (*repo.User, error) {
	key := fmt.Sprintf("users:%s", id)
	data, err := u.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var user repo.User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (u *Users) Set(ctx context.Context, id string, user *repo.User, ttl ...time.Duration) error {
	key := fmt.Sprintf("users:%s", id)
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		return u.client.Set(ctx, key, data, ttl[0]).Err()
	}
	return u.client.Set(ctx, key, data, 0).Err()
}

func (u *Users) Del(ctx context.Context, id string) error {
	key := fmt.Sprintf("users:%s", id)
	return u.client.Del(ctx, key).Err()
}
