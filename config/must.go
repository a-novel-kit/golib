package config

// Must returns value, or panics when err is non-nil. Use it at initialization, where a failure is
// unrecoverable and should stop the program.
//
// Prefer lo.Must from github.com/samber/lo, which does the same. A later release removes Must once no
// consumer calls it.
func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}

// MustUnmarshal decodes src into a T using unmarshal, panicking if decoding fails.
func MustUnmarshal[T any](unmarshal func([]byte, any) error, src []byte) T {
	var value T

	err := unmarshal(src, &value)
	if err != nil {
		panic(err)
	}

	return value
}
