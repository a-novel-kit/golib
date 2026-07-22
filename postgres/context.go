package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/uptrace/bun"
)

// ContextKey is the context value key under which the package stores the active
// bun.IDB connection.
type ContextKey struct{}

// NameLen caps the length of a generated schema or database name, keeping it
// within PostgreSQL's 63-byte identifier limit with room for prefixes.
const NameLen = 31

// ErrNoIDBInContext is returned by GetContext when the context carries no
// bun.IDB, meaning it was never seeded by NewContext or one of its variants.
// [ErrNoDbInContext] covers the stricter case where a connection is present but
// is not a full *bun.DB.
var ErrNoIDBInContext = errors.New("context does not contain a bun.IDB")

// NewContext derives a context carrying a connection to the primary database,
// for later retrieval with GetContext.
func NewContext(ctx context.Context, config Config) (context.Context, error) {
	db, err := config.DB(ctx)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), nil
}

// NewContextSchema derives a context carrying a connection scoped to the named
// schema, creating the schema first when create is true.
func NewContextSchema(ctx context.Context, config Config, schema string, create bool) (context.Context, error) {
	db, err := config.DBSchema(ctx, schema, create)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), nil
}

// GetContext returns the bun.IDB stored in ctx by NewContext or one of its
// variants, or ErrNoIDBInContext when none is present.
func GetContext(ctx context.Context) (bun.IDB, error) {
	db, ok := ctx.Value(ContextKey{}).(bun.IDB)
	if !ok {
		return nil, ErrNoIDBInContext
	}

	return db, nil
}

// TransferContext transfers the current postgres context into another. If the source context is not a postgres
// context, this is a no-op.
func TransferContext(baseCtx, destCtx context.Context) context.Context {
	db, ok := baseCtx.Value(ContextKey{}).(bun.IDB)
	if !ok {
		return destCtx
	}

	return context.WithValue(destCtx, ContextKey{}, db)
}
