package postgres

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/plugin-tests/internal/storage"
	"github.com/vultisig/plugin-tests/internal/storage/postgres/queries"
)

var _ storage.DatabaseStorage = (*PostgresBackend)(nil)

//go:embed migrations/*.sql
var migrations embed.FS

type PostgresBackend struct {
	pool    *pgxpool.Pool
	queries *queries.Queries
}

func NewPostgresBackend(dsn string) (*PostgresBackend, error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	backend := &PostgresBackend{
		pool:    pool,
		queries: queries.New(pool),
	}

	err = backend.Migrate()
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return backend, nil
}

func (p *PostgresBackend) Migrate() error {
	logrus.Info("Starting database migration...")
	goose.SetLogger(logrus.StandardLogger())
	goose.SetBaseFS(migrations)
	defer goose.SetBaseFS(nil)

	err := goose.SetDialect("postgres")
	if err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(p.pool)
	defer db.Close()

	err = goose.Up(db, "migrations", goose.WithAllowMissing())
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	logrus.Info("Database migration completed successfully")
	return nil
}

func (p *PostgresBackend) Queries() *queries.Queries {
	return p.queries
}

func (p *PostgresBackend) Pool() *pgxpool.Pool {
	return p.pool
}

func (p *PostgresBackend) Close() error {
	p.pool.Close()
	return nil
}
