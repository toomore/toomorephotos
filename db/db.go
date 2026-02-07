package db

import (
	"context"
	"embed"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaFS embed.FS

// DB wraps PostgreSQL connection pool.
type DB struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL and returns a DB instance.
// Returns nil, nil if addr is empty (DATABASE_URL not set).
func Open(ctx context.Context, addr string) (*DB, error) {
	if addr == "" {
		return nil, nil
	}
	config, err := pgxpool.ParseConfig(addr)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	log.Println("DB: connected to PostgreSQL")
	return &DB{pool: pool}, nil
}

// Close closes the connection pool.
func (d *DB) Close() {
	if d != nil && d.pool != nil {
		d.pool.Close()
	}
}

// InitSchema runs schema.sql to create tables if they don't exist.
func (d *DB) InitSchema(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return nil
	}
	sql, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	_, err = d.pool.Exec(ctx, string(sql))
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	log.Println("DB: schema initialized")
	return nil
}

// Pool returns the underlying pool for direct queries (used by photos.go).
func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}
