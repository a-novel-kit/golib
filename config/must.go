package config

// MustUnmarshal decodes src into a T using unmarshal, panicking if decoding fails.
func MustUnmarshal[T any](unmarshal func([]byte, any) error, src []byte) T {
	var value T

	err := unmarshal(src, &value)
	if err != nil {
		panic(err)
	}

	return value
}
