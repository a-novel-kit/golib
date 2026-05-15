package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uptrace/bun"
)

type ContextKey struct{}

const NameLen = 31

// ErrNoIDBInContext is returned by GetContext when the supplied context does
// not carry a bun.IDB value (it was never seeded via NewContext / one of its
// variants). It is sibling to ErrNoDbInContext in migrator.go, which signals
// the stricter "found an IDB but it isn't a *bun.DB" case.
var ErrNoIDBInContext = errors.New("context does not contain a bun.IDB")

func NewContext(ctx context.Context, config Config) (context.Context, error) {
	db, err := config.DB(ctx)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), nil
}

func NewContextSchema(ctx context.Context, config Config, schema string, create bool) (context.Context, error) {
	db, err := config.DBSchema(ctx, schema, create)
	if err != nil {
		return nil, fmt.Errorf("get db from config: %w", err)
	}

	return context.WithValue(ctx, ContextKey{}, db), nil
}

func GetContext(ctx context.Context) (bun.IDB, error) {
	db, ok := ctx.Value(ContextKey{}).(bun.IDB)
	if !ok {
		return nil, ErrNoIDBInContext
	}

	return db, nil
}

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
