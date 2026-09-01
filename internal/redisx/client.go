package redisx

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type ClientOptions struct {
	Addr     string
	Password string
	DB       int
	Timeout  time.Duration
}

type Store struct {
	client  *redis.Client
	timeout time.Duration
}

func NewClient(options ClientOptions) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         options.Addr,
		Password:     options.Password,
		DB:           options.DB,
		DialTimeout:  options.Timeout,
		ReadTimeout:  options.Timeout,
		WriteTimeout: options.Timeout,
	})
}

func NewStore(client *redis.Client, timeout time.Duration) *Store {
	return &Store{client: client, timeout: timeout}
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}
