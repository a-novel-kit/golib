package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/uptrace/bun"
)

// contextKey is the context value key under which the package stores the active bun.IDB connection.
type contextKey struct{}

// ContextKey is the context value key under which the package stores the active bun.IDB connection.
//
// Prefer [WithTx] to put a connection on a context, which v0.65.0 already has, and [GetContext] to
// read it. A later release unexports the key once no consumer names it.
type ContextKey = contextKey

// ErrNoIDBInContext is returned by GetContext when the context carries no
// bun.IDB, meaning it was never seeded by NewContext or one of its variants.
// [ErrNoDBInContext] covers the stricter case where a connection is present but
// is not a full *bun.DB.
var ErrNoIDBInContext = errors.New("context does not contain a bun.IDB")

// ErrNoDBInContext is returned by an operation that needs the connection pool, such as
// [RunMigrationsContext] or [Health], when the context carries a transaction instead.
// [ErrNoIDBInContext] covers the case of no connection at all.
var ErrNoDBInContext = errors.New("context does not contain a bun.DB")

// NewContext derives a context carrying a connection to the primary database,
// for later retrieval with GetContext.
func NewContext(ctx context.Context, config Config) (context.Context, error) {
	db, err := config.DB(ctx)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, contextKey{}, db), nil
}

// GetContext returns the bun.IDB stored in ctx by NewContext or one of its
// variants, or ErrNoIDBInContext when none is present.
func GetContext(ctx context.Context) (bun.IDB, error) {
	db, ok := ctx.Value(contextKey{}).(bun.IDB)
	if !ok {
		return nil, ErrNoIDBInContext
	}

	return db, nil
}

// TransferContext copies the database handle baseCtx carries onto destCtx, such as onto a detached
// context a background task runs under. It returns destCtx unchanged when baseCtx carries none.
//
// Prefer a gRPC server from grpcf.NewGRPCServer with a nil baseContext, which gives each call the values
// of the process context, the database handle included. A background task keeps them under
// context.WithoutCancel. A later release removes TransferContext once no consumer calls it.
func TransferContext(baseCtx, destCtx context.Context) context.Context {
	db, ok := baseCtx.Value(contextKey{}).(bun.IDB)
	if !ok {
		return destCtx
	}

	return context.WithValue(destCtx, contextKey{}, db)
}
