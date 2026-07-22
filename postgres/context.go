package postgres

import (
	"context"
	"database/sql"
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

// RunInTx runs callback within a transaction opened on the connection carried
// by ctx. The callback receives the transaction as a bun.IDB.
//
// The context it hands the callback is the original one, still carrying the
// pool — so anything inside that resolves its handle with [GetContext] gets the
// pool back and commits independently of the surrounding transaction, silently.
// Only calls made through the tx argument take part, and a caller who threads
// ctx instead has a block that opens a transaction, commits it, and protects
// nothing. Nothing about that fails or logs.
//
// Deprecated: use [WithinTx], whose callback takes no transaction argument
// because the transaction is installed on the context it receives. With nothing
// to thread, the mistake above cannot be written.
func RunInTx(ctx context.Context, opts *sql.TxOptions, callback func(ctx context.Context, tx bun.IDB) error) error {
	db, err := GetContext(ctx)
	if err != nil {
		return fmt.Errorf("get db from context: %w", err)
	}

	return db.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		return callback(ctx, tx)
	})
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
