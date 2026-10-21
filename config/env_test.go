package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/config"
)

func TestStringParser(t *testing.T) {
	t.Setenv("foo", "foo:value")

	require.Equal(t, "foo:value", config.LoadEnv(os.Getenv("foo"), "foo:default", config.StringParser))
	// bar is unset, and StringParser cannot fail regardless.
	require.Equal(t, "bar:default", config.LoadEnv(os.Getenv("bar"), "bar:default", config.StringParser))
}

func TestInt64Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, int64(123), config.LoadEnv(os.Getenv("foo"), 321, config.Int64Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Int64Parser) })
	require.Equal(t, int64(321), config.LoadEnv(os.Getenv("qux"), 321, config.Int64Parser))
}

func TestInt32Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, int32(123), config.LoadEnv(os.Getenv("foo"), 321, config.Int32Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Int32Parser) })
	require.Equal(t, int32(321), config.LoadEnv(os.Getenv("qux"), 321, config.Int32Parser))
}

func TestInt16Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, int16(123), config.LoadEnv(os.Getenv("foo"), 321, config.Int16Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Int16Parser) })
	require.Equal(t, int16(321), config.LoadEnv(os.Getenv("qux"), 321, config.Int16Parser))
}

func TestInt8Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, int8(123), config.LoadEnv(os.Getenv("foo"), 64, config.Int8Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 64, config.Int8Parser) })
	require.Equal(t, int8(64), config.LoadEnv(os.Getenv("qux"), 64, config.Int8Parser))
}

func TestIntParser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, 123, config.LoadEnv(os.Getenv("foo"), 321, config.IntParser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.IntParser) })
	require.Equal(t, 321, config.LoadEnv(os.Getenv("qux"), 321, config.IntParser))
}

func TestUint64Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, uint64(123), config.LoadEnv(os.Getenv("foo"), 321, config.Uint64Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Uint64Parser) })
	require.Equal(t, uint64(321), config.LoadEnv(os.Getenv("qux"), 321, config.Uint64Parser))
}

func TestUint32Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, uint32(123), config.LoadEnv(os.Getenv("foo"), 321, config.Uint32Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Uint32Parser) })
	require.Equal(t, uint32(321), config.LoadEnv(os.Getenv("qux"), 321, config.Uint32Parser))
}

func TestUint16Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, uint16(123), config.LoadEnv(os.Getenv("foo"), 321, config.Uint16Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Uint16Parser) })
	require.Equal(t, uint16(321), config.LoadEnv(os.Getenv("qux"), 321, config.Uint16Parser))
}

func TestUint8Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, uint8(123), config.LoadEnv(os.Getenv("foo"), 64, config.Uint8Parser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 64, config.Uint8Parser) })
	require.Equal(t, uint8(64), config.LoadEnv(os.Getenv("qux"), 64, config.Uint8Parser))
}

func TestUintParser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.Equal(t, uint(123), config.LoadEnv(os.Getenv("foo"), 321, config.UintParser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.UintParser) })
	require.Equal(t, uint(321), config.LoadEnv(os.Getenv("qux"), 321, config.UintParser))
}

func TestBoolParser(t *testing.T) {
	t.Setenv("foo", "false")
	t.Setenv("bar", "bar:value")

	require.False(t, config.LoadEnv(os.Getenv("foo"), true, config.BoolParser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), true, config.BoolParser) })
	require.True(t, config.LoadEnv(os.Getenv("qux"), true, config.BoolParser))
}

func TestFloat64Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.InEpsilon(t, float64(123), config.LoadEnv(os.Getenv("foo"), 321, config.Float64Parser), 0.0001)
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Float64Parser) })
	require.InEpsilon(t, float64(321), config.LoadEnv(os.Getenv("qux"), 321, config.Float64Parser), 0.0001)
}

func TestFloat32Parser(t *testing.T) {
	t.Setenv("foo", "123")
	t.Setenv("bar", "bar:value")

	require.InEpsilon(t, float32(123), config.LoadEnv(os.Getenv("foo"), 321, config.Float32Parser), 0.0001)
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), 321, config.Float32Parser) })
	require.InEpsilon(t, float32(321), config.LoadEnv(os.Getenv("qux"), 321, config.Float32Parser), 0.0001)
}

func TestDurationParser(t *testing.T) {
	t.Setenv("foo", "5s")
	t.Setenv("bar", "bar:value")

	require.Equal(t, 5*time.Second, config.LoadEnv(os.Getenv("foo"), time.Second, config.DurationParser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), time.Second, config.DurationParser) })
	require.Equal(t, time.Second, config.LoadEnv(os.Getenv("qux"), time.Second, config.DurationParser))
}

func TestTimeParser(t *testing.T) {
	t.Setenv("foo", "2020-01-02T15:04:05Z")
	t.Setenv("bar", "bar:value")

	now := time.Now()
	custom := time.Date(2020, 1, 2, 15, 4, 5, 0, time.UTC)

	require.Equal(t, custom, config.LoadEnv(os.Getenv("foo"), now, config.TimeParser))
	require.Panics(t, func() { _ = config.LoadEnv(os.Getenv("bar"), now, config.TimeParser) })
	require.Equal(t, now, config.LoadEnv(os.Getenv("qux"), now, config.TimeParser))
}

func TestStringSliceParser(t *testing.T) {
	t.Setenv("foo", "foo,bar,baz")

	basic := []string{"foo", "bar", "qux"}
	custom := []string{"foo", "bar", "baz"}

	require.Equal(t, custom, config.LoadEnv(os.Getenv("foo"), basic, config.SliceParser(config.StringParser)))
	// bar is unset here, so the fallback stands.
	require.Equal(t, basic, config.LoadEnv(os.Getenv("bar"), basic, config.SliceParser(config.StringParser)))
}

func TestEnumParser(t *testing.T) {
	t.Setenv("foo", "bar")

	require.Equal(
		t,
		"bar",
		config.LoadEnv(os.Getenv("foo"), "invalid", config.EnumParser(config.StringParser, "foo", "bar")),
	)
	// A value outside the allow list is set-but-wrong, not absent.
	require.Panics(t, func() {
		_ = config.LoadEnv(os.Getenv("foo"), "invalid", config.EnumParser(config.StringParser, "foo", "baz"))
	})
}

func TestLoadEnv(t *testing.T) {
	// The panic carries the offending value, the type it could not become, and the parser's own
	// error. LoadEnv never sees the variable's name, so those three are what locate the line to fix.
	t.Run("Error/PanicNamesTheValueAndType", func(t *testing.T) {
		t.Setenv("foo", "on")

		// OTEL=on is the live instance: no bool strconv recognizes, and the fallback selects the
		// preset that turns every exporter off.
		err := requirePanicError(t, func() {
			_ = config.LoadEnv(os.Getenv("foo"), false, config.BoolParser)
		})

		require.ErrorContains(t, err, `"on"`)
		require.ErrorContains(t, err, "bool")
		require.ErrorContains(t, err, "invalid syntax", "the parser's own error must survive")
	})
}

func requirePanicError(t *testing.T, fn func()) (err error) {
	t.Helper()

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered, "expected a panic")

		var ok bool

		err, ok = recovered.(error)
		require.True(t, ok, "expected the panic to carry an error, got %T", recovered)
	}()

	fn()

	return nil
}
