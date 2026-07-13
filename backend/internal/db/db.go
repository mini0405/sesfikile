package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

// New creates a connection pool without eagerly connecting. pgxpool.New only
// parses the DSN and prepares the pool; it does not dial the database until a
// query or Ping is executed, so this succeeds even if Postgres is down.
func New(databaseURL string) (*DB, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Ping(ctx context.Context) error {
	return d.Pool.Ping(ctx)
}

func (d *DB) Close() {
	d.Pool.Close()
}
