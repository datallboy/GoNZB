package pgindex

import (
	"context"
	"database/sql"
)

type federationDBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type federationTransactionContextKey struct{}

func withFederationTransaction(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, federationTransactionContextKey{}, tx)
}

func (s *Store) federationExecutor(ctx context.Context) federationDBTX {
	if tx, ok := ctx.Value(federationTransactionContextKey{}).(*sql.Tx); ok && tx != nil {
		return tx
	}
	return s.db
}

func (s *Store) beginFederationProjection(ctx context.Context) (*sql.Tx, func() error, func(), error) {
	if tx, ok := ctx.Value(federationTransactionContextKey{}).(*sql.Tx); ok && tx != nil {
		return tx, func() error { return nil }, func() {}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return tx, tx.Commit, func() { _ = tx.Rollback() }, nil
}
