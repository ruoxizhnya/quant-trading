package live

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type RedisOrderStoreConfig struct {
	Prefix     string
	KeyExpiry  time.Duration
}

type RedisOrderStore struct {
	client redis.Cmdable
	config RedisOrderStoreConfig
	logger zerolog.Logger
}

func NewRedisOrderStore(client redis.Cmdable, config RedisOrderStoreConfig, logger zerolog.Logger) *RedisOrderStore {
	if config.Prefix == "" {
		config.Prefix = "order:"
	}
	if config.KeyExpiry == 0 {
		config.KeyExpiry = 7 * 24 * time.Hour // 7 days default TTL
	}
	return &RedisOrderStore{
		client: client,
		config: config,
		logger: logger.With().Str("component", "redis_order_store").Logger(),
	}
}

func (s *RedisOrderStore) orderKey(orderID string) string {
	return s.config.Prefix + orderID
}

func (s *RedisOrderStore) Save(ctx context.Context, order *OrderRecord) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	key := s.orderKey(order.OrderID)
	if err := s.client.Set(ctx, key, data, s.config.KeyExpiry).Err(); err != nil {
		return fmt.Errorf("failed to save order to Redis: %w", err)
	}

	s.logger.Debug().
		Str("order_id", order.OrderID).
		Str("symbol", order.Symbol).
		Str("status", string(order.Status)).
		Msg("Order saved to Redis")

	return nil
}

func (s *RedisOrderStore) Get(ctx context.Context, orderID string) (*OrderRecord, error) {
	key := s.orderKey(orderID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("failed to get order from Redis: %w", err)
	}

	var order OrderRecord
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("failed to unmarshal order: %w", err)
	}

	return &order, nil
}

func (s *RedisOrderStore) List(ctx context.Context, symbol string, status OrderStatus) ([]*OrderRecord, error) {
	pattern := s.config.Prefix + "*"
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list order keys from Redis: %w", err)
	}

	var orders []*OrderRecord
	for _, key := range keys {
		data, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("Failed to get order data")
			continue
		}

		var order OrderRecord
		if err := json.Unmarshal(data, &order); err != nil {
			s.logger.Warn().Err(err).Str("key", key).Msg("Failed to unmarshal order")
			continue
		}

		if symbol != "" && order.Symbol != symbol {
			continue
		}
		if status != "" && order.Status != status {
			continue
		}

		orders = append(orders, &order)
	}

	s.logger.Debug().
		Int("count", len(orders)).
		Str("symbol_filter", symbol).
		Str("status_filter", string(status)).
		Msg("Orders listed from Redis")

	return orders, nil
}

func (s *RedisOrderStore) Update(ctx context.Context, orderID string, updates map[string]interface{}) error {
	order, err := s.Get(ctx, orderID)
	if err != nil {
		return err
	}

	if status, ok := updates["status"]; ok {
		order.Status = status.(OrderStatus)
	}
	if filledQty, ok := updates["filled_qty"]; ok {
		order.FilledQty = filledQty.(float64)
	}
	if avgFillPrice, ok := updates["avg_fill_price"]; ok {
		order.AvgFillPrice = avgFillPrice.(float64)
	}
	if message, ok := updates["message"]; ok {
		order.Message = message.(string)
	}
	order.UpdatedAt = time.Now().Unix()

	return s.Save(ctx, order)
}

func (s *RedisOrderStore) Delete(ctx context.Context, orderID string) error {
	key := s.orderKey(orderID)
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete order from Redis: %w", err)
	}

	s.logger.Debug().Str("order_id", orderID).Msg("Order deleted from Redis")
	return nil
}

var ErrOrderNotFound = fmt.Errorf("order not found")
