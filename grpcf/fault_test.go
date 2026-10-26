package grpcf_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/grpcf"
)

func TestFault(t *testing.T) {
	t.Parallel()

	errCause := errors.New("dial tcp db.internal:5432: password authentication failed")

	testCases := []struct {
		name string

		code codes.Code
		msg  string
	}{
		{name: "Internal", code: codes.Internal, msg: "internal error"},
		{name: "Unavailable", code: codes.Unavailable, msg: "stream interrupted: resume after the last event received"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := grpcf.Fault(testCase.code, testCase.msg, errCause)

			// The caller receives the status alone, while the chain keeps the cause.
			answer, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, testCase.code, answer.Code())
			require.Equal(t, testCase.msg, answer.Message())
			require.NotContains(t, err.Error(), "password")
			require.ErrorIs(t, err, errCause)
		})
	}
}
