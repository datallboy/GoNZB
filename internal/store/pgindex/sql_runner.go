package pgindex

import (
	"context"
	"database/sql"
)

type sqlExecQueryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type sqlExecQueryRower interface {
	sqlExecQueryer
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const postgresBindParameterSoftLimit = 50000
