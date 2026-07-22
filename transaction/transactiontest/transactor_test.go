package transactiontest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/transaction"
	"github.com/a-novel-kit/golib/transaction/transactiontest"
)

var errTest = errors.New("test")

func TestTransactorRunsTheCallback(t *testing.T) {
	t.Parallel()

	transactor := transactiontest.NewTransactor()
	ran := false

	err := transactor.WithinTx(t.Context(), func(context.Context) error {
		ran = true

		return nil
	})
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 1, transactor.Calls())
}

func TestTransactorPropagatesTheCallbackError(t *testing.T) {
	t.Parallel()

	transactor := transactiontest.NewTransactor()

	err := transactor.WithinTx(t.Context(), func(context.Context) error {
		return errTest
	})
	require.ErrorIs(t, err, errTest)
}

// TestFailingTransactorSkipsTheCallback covers the property the failing double
// exists for: an operation that reports success when its unit of work never
// started is the bug it reproduces, so the callback must not run.
func TestFailingTransactorSkipsTheCallback(t *testing.T) {
	t.Parallel()

	transactor := transactiontest.NewFailingTransactor(errTest)

	err := transactor.WithinTx(t.Context(), func(context.Context) error {
		t.Error("the callback ran despite the transaction failing to open")

		return nil
	})
	require.ErrorIs(t, err, errTest)
	require.Equal(t, 1, transactor.Calls())
}

func TestTransactorCountsNestedCalls(t *testing.T) {
	t.Parallel()

	transactor := transactiontest.NewTransactor()

	err := transactor.WithinTx(t.Context(), func(ctx context.Context) error {
		return transactor.WithinTx(ctx, func(context.Context) error {
			return nil
		})
	})
	require.NoError(t, err)
	require.Equal(t, 2, transactor.Calls())
}

// TestTransactorSatisfiesTheInterface fails to compile rather than fails to
// run if the double drifts from the contract it stands in for.
func TestTransactorSatisfiesTheInterface(t *testing.T) {
	t.Parallel()

	var transactor transaction.Transactor = transactiontest.NewTransactor()

	require.NoError(t, transactor.WithinTx(t.Context(), func(context.Context) error { return nil }))
}
