package config_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/config"
)

func TestLoadEnv(t *testing.T) {
	t.Parallel()

	stringSlice := config.SliceParser(config.StringParser)

	testCases := []struct {
		name string

		load func() any

		expect any
		// expectPanic lists what the panic error must say. LoadEnv never sees the variable's name, so
		// the value, the type it could not become and the parser's own error locate the line to fix.
		expectPanic []string
	}{
		{
			name:   "Success/Value",
			load:   func() any { return config.LoadEnv("value", "default", config.StringParser) },
			expect: "value",
		},
		{
			name:   "Success/UnsetKeepsFallback",
			load:   func() any { return config.LoadEnv("", "default", config.StringParser) },
			expect: "default",
		},
		{
			name:   "Success/Int64",
			load:   func() any { return config.LoadEnv("0x10", int64(1), config.Int64Parser) },
			expect: int64(16),
		},
		{
			name:   "Success/SliceTrimsAndSkipsEmptyElements",
			load:   func() any { return config.LoadEnv(" foo, bar,,baz ", []string{"qux"}, stringSlice) },
			expect: []string{"foo", "bar", "baz"},
		},
		{
			name:   "Success/SliceEmptyLiteral",
			load:   func() any { return config.LoadEnv("[]", []string{"qux"}, stringSlice) },
			expect: []string(nil),
		},
		{
			// OTEL=on is the live instance: no bool strconv recognizes, and the fallback selects the
			// preset that turns every exporter off.
			name:        "Error/NamesTheValueAndType",
			load:        func() any { return config.LoadEnv("on", false, strconv.ParseBool) },
			expectPanic: []string{`"on"`, "bool", "invalid syntax"},
		},
		{
			name:        "Error/Int64",
			load:        func() any { return config.LoadEnv("one", int64(1), config.Int64Parser) },
			expectPanic: []string{`"one"`, "int64"},
		},
		{
			name: "Error/SliceElement",
			load: func() any {
				return config.LoadEnv("1,one", []int64{2}, config.SliceParser(config.Int64Parser))
			},
			expectPanic: []string{`"1,one"`, "[]int64"},
		},
		{
			name:        "Error/SliceWithoutElement",
			load:        func() any { return config.LoadEnv(" , ", []string{"qux"}, stringSlice) },
			expectPanic: []string{"empty"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if len(testCase.expectPanic) == 0 {
				require.Equal(t, testCase.expect, testCase.load())

				return
			}

			err := requirePanicError(t, func() { _ = testCase.load() })
			for _, expect := range testCase.expectPanic {
				require.ErrorContains(t, err, expect)
			}
		})
	}
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
