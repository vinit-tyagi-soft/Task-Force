// Package store owns database connectivity, migrations and scoped transactions.
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scope is the authenticated context applied to row-level security for one transaction.
type Scope struct {
	UserID   uuid.UUID
	TenantID *uuid.UUID
}

type Store struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// InTx runs fn inside a transaction whose RLS context is set with transaction-local
// settings, so the context is discarded when the pooled connection is returned.
func (s *Store) InTx(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tenant := ""
	if scope.TenantID != nil {
		tenant = scope.TenantID.String()
	}
	user := ""
	if scope.UserID != uuid.Nil {
		user = scope.UserID.String()
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true), set_config('app.tenant_id', $2, true)`, user, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Migrate applies embedded *.sql files in lexical order, once each.
func Migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// ConstraintMessage returns the database message for check/trigger violations that are safe to show.
func ConstraintMessage(err error) (string, bool) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23503") {
		msg := pg.Message
		if strings.HasPrefix(msg, "new row for relation") || strings.Contains(msg, "violates") {
			return "The change violates a data integrity rule.", true
		}
		return strings.ToUpper(msg[:1]) + msg[1:] + ".", true
	}
	return "", false
}
