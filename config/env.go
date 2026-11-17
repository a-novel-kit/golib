package config

import (
	"fmt"
	"strconv"
	"strings"
)

// LoadEnv parses the raw environment value into T using parser.
//
// An empty value yields fallback. A value that is set but does not parse panics, naming the value
// and the type it could not become. Configuration is read at boot from package-level vars, so the
// panic precedes any traffic.
func LoadEnv[T any](value string, fallback T, parser func(string) (T, error)) T {
	if value == "" {
		return fallback
	}

	parsedValue, err := parser(value)
	if err != nil {
		panic(fmt.Errorf("(LoadEnv) cannot parse %q as %T: %w", value, fallback, err))
	}

	return parsedValue
}

// SliceParser builds a [LoadEnv] parser for a comma-separated list, delegating each trimmed
// element to parser. One bad element fails the whole list, and so does a value holding no element.
// The literal "[]" yields an empty list, overriding the fallback.
func SliceParser[T any](parser func(string) (T, error)) func(string) ([]T, error) {
	return func(value string) ([]T, error) {
		return parseSlice(value, parser)
	}
}

// parseSlice parses value as the parser [SliceParser] builds.
func parseSlice[T any](value string, parser func(string) (T, error)) ([]T, error) {
	if value == "[]" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	parsedValues := make([]T, 0, len(parts))

	for _, part := range parts {
		trimmedPart := strings.TrimSpace(part)
		if trimmedPart == "" {
			continue
		}

		parsedValue, err := parser(trimmedPart)
		if err != nil {
			return nil, err
		}

		parsedValues = append(parsedValues, parsedValue)
	}

	if len(parsedValues) == 0 {
		return nil, fmt.Errorf(`value "%s" is empty`, value)
	}

	return parsedValues, nil
}

// StringParser returns the raw value unchanged, for [LoadEnv].
func StringParser(value string) (string, error) {
	return value, nil
}

// Int64Parser parses an int64 for [LoadEnv], accepting the formats of strconv.ParseInt.
func Int64Parser(value string) (int64, error) {
	return strconv.ParseInt(value, 0, 64)
}
