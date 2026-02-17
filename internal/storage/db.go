package storage

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

type DatabaseStorage interface {
	Queries() *queries.Queries
	Pool() *pgxpool.Pool
	Close() error
}
