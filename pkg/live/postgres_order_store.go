package live

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type PostgresOrderStoreConfig struct {
	SchemaName string
	TableName  string
}

type PostgresOrderStore struct {
	pool   *pgxpool.Pool
	config PostgresOrderStoreConfig
	logger zerolog.Logger
}

func NewPostgresOrderStore(pool *pgxpool.Pool, config PostgresOrderStoreConfig, logger zerolog.Logger) *PostgresOrderStore {
	if config.SchemaName == "" {
		config.SchemaName = "public"
	}
	if config.TableName == "" {
		config.TableName = "orders"
	}
	return &PostgresOrderStore{
		pool:   pool,
		config: config,
		logger: logger.With().Str("component", "postgres_order_store").Logger(),
	}
}

func (s *PostgresOrderStore) tableName() string {
	return fmt.Sprintf("%s.%s", s.config.SchemaName, s.config.TableName)
}

func (s *PostgresOrderStore) Save(ctx context.Context, order *OrderRecord) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (order_id, symbol, direction, order_type, quantity, filled_qty, price, avg_fill_price, status, submitted_at, updated_at, message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (order_id) DO UPDATE SET
			symbol = EXCLUDED.symbol,
			direction = EXCLUDED.direction,
			order_type = EXCLUDED.order_type,
			quantity = EXCLUDED.quantity,
			filled_qty = EXCLUDED.filled_qty,
			price = EXCLUDED.price,
			avg_fill_price = EXCLUDED.avg_fill_price,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at,
			message = EXCLUDED.message
	`, s.tableName())

	_, err := s.pool.Exec(ctx, query,
		order.OrderID,
		order.Symbol,
		order.Direction,
		order.OrderType,
		order.Quantity,
		order.FilledQty,
		order.Price,
		order.AvgFillPrice,
		string(order.Status),
		order.SubmittedAt,
		time.Now().Unix(),
		order.Message,
	)
	if err != nil {
		return fmt.Errorf("failed to save order to Postgres: %w", err)
	}

	s.logger.Debug().
		Str("order_id", order.OrderID).
		Str("symbol", order.Symbol).
		Str("status", string(order.Status)).
		Msg("Order saved to Postgres")

	return nil
}

func (s *PostgresOrderStore) Get(ctx context.Context, orderID string) (*OrderRecord, error) {
	query := fmt.Sprintf(`
		SELECT order_id, symbol, direction, order_type, quantity, filled_qty, price, avg_fill_price, status, submitted_at, updated_at, message
		FROM %s WHERE order_id = $1
	`, s.tableName())

	var order OrderRecord
	err := s.pool.QueryRow(ctx, query, orderID).Scan(
		&order.OrderID,
		&order.Symbol,
		&order.Direction,
		&order.OrderType,
		&order.Quantity,
		&order.FilledQty,
		&order.Price,
		&order.AvgFillPrice,
		&order.Status,
		&order.SubmittedAt,
		&order.UpdatedAt,
		&order.Message,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("failed to get order from Postgres: %w", err)
	}

	return &order, nil
}

func (s *PostgresOrderStore) List(ctx context.Context, symbol string, status OrderStatus) ([]*OrderRecord, error) {
	query := fmt.Sprintf(`
		SELECT order_id, symbol, direction, order_type, quantity, filled_qty, price, avg_fill_price, status, submitted_at, updated_at, message
		FROM %s WHERE 1=1
	`, s.tableName())

	args := []interface{}{}
	argIdx := 1

	if symbol != "" {
		query += fmt.Sprintf(" AND symbol = $%d", argIdx)
		args = append(args, symbol)
		argIdx++
	}

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(status))
		argIdx++
	}

	query += " ORDER BY submitted_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list orders from Postgres: %w", err)
	}
	defer rows.Close()

	var orders []*OrderRecord
	for rows.Next() {
		var order OrderRecord
		if err := rows.Scan(
			&order.OrderID,
			&order.Symbol,
			&order.Direction,
			&order.OrderType,
			&order.Quantity,
			&order.FilledQty,
			&order.Price,
			&order.AvgFillPrice,
			&order.Status,
			&order.SubmittedAt,
			&order.UpdatedAt,
			&order.Message,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order row: %w", err)
		}
		orders = append(orders, &order)
	}

	s.logger.Debug().
		Int("count", len(orders)).
		Str("symbol_filter", symbol).
		Str("status_filter", string(status)).
		Msg("Orders listed from Postgres")

	return orders, nil
}

func (s *PostgresOrderStore) Update(ctx context.Context, orderID string, updates map[string]interface{}) error {
	setClauses := []string{}
	args := []interface{}{orderID}
	argIdx := 2

	for field, value := range updates {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", field, argIdx))
		args = append(args, value)
		argIdx++
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now().Unix())

	query := fmt.Sprintf("UPDATE %s SET %s WHERE order_id = $1", s.tableName(), joinStrings(setClauses, ", "))
	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update order in Postgres: %w", err)
	}

	s.logger.Debug().Str("order_id", orderID).Msg("Order updated in Postgres")
	return nil
}

func (s *PostgresOrderStore) Delete(ctx context.Context, orderID string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE order_id = $1", s.tableName())
	_, err := s.pool.Exec(ctx, query, orderID)
	if err != nil {
		return fmt.Errorf("failed to delete order from Postgres: %w", err)
	}

	s.logger.Debug().Str("order_id", orderID).Msg("Order deleted from Postgres")
	return nil
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
