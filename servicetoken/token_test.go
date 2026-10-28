package servicetoken_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/servicetoken"
)

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(path, []byte("header.payload.signature\n"), 0o600))

		token, err := servicetoken.Read(path)
		require.NoError(t, err)
		require.Equal(t, "header.payload.signature", token)
	})

	t.Run("Error/Missing", func(t *testing.T) {
		t.Parallel()

		_, err := servicetoken.Read(filepath.Join(t.TempDir(), "token"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
