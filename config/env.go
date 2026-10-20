package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
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

// EnumParser wraps another parser and rejects any parsed value absent from the allow list.
func EnumParser[T comparable](parser func(string) (T, error), allow ...T) func(string) (T, error) {
	return func(value string) (T, error) {
		raw, err := parser(value)
		if err != nil || slices.Contains(allow, raw) {
			return raw, err
		}

		return raw, fmt.Errorf(`value "%s" is not allowed`, value)
	}
}

// Int64Parser parses an int64 for [LoadEnv], accepting the formats of strconv.ParseInt.
func Int64Parser(value string) (int64, error) {
	return strconv.ParseInt(value, 0, 64)
}

// Int32Parser parses an int32 for [LoadEnv], accepting the formats of strconv.ParseInt.
func Int32Parser(value string) (int32, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 32)

	return int32(parsedValue), err
}

// Int16Parser parses an int16 for [LoadEnv], accepting the formats of strconv.ParseInt.
func Int16Parser(value string) (int16, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 16)

	return int16(parsedValue), err
}

// Int8Parser parses an int8 for [LoadEnv], accepting the formats of strconv.ParseInt.
func Int8Parser(value string) (int8, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 8)

	return int8(parsedValue), err
}

// IntParser parses an int for [LoadEnv], accepting the formats of strconv.Atoi.
func IntParser(value string) (int, error) {
	return strconv.Atoi(value)
}

// Uint64Parser parses a uint64 for [LoadEnv], accepting the formats of strconv.ParseUint.
func Uint64Parser(value string) (uint64, error) {
	return strconv.ParseUint(value, 0, 64)
}

// Uint32Parser parses a uint32 for [LoadEnv], accepting the formats of strconv.ParseUint.
func Uint32Parser(value string) (uint32, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 32)

	return uint32(parsedValue), err
}

// Uint16Parser parses a uint16 for [LoadEnv], accepting the formats of strconv.ParseUint.
func Uint16Parser(value string) (uint16, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 16)

	return uint16(parsedValue), err
}

// Uint8Parser parses a uint8 for [LoadEnv], accepting the formats of strconv.ParseUint.
func Uint8Parser(value string) (uint8, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 8)

	return uint8(parsedValue), err
}

// UintParser parses a uint for [LoadEnv], accepting the formats of strconv.ParseUint.
func UintParser(value string) (uint, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 0)

	return uint(parsedValue), err
}

// BoolParser parses a bool for [LoadEnv], accepting the formats of strconv.ParseBool.
func BoolParser(value string) (bool, error) {
	return strconv.ParseBool(value)
}

// Float64Parser parses a float64 for [LoadEnv], accepting the formats of strconv.ParseFloat.
func Float64Parser(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}

// Float32Parser parses a float32 for [LoadEnv], accepting the formats of strconv.ParseFloat.
func Float32Parser(value string) (float32, error) {
	parsedValue, err := strconv.ParseFloat(value, 32)

	return float32(parsedValue), err
}

// DurationParser parses a time.Duration for [LoadEnv], accepting the formats of time.ParseDuration.
func DurationParser(value string) (time.Duration, error) {
	return time.ParseDuration(value)
}

// TimeParser parses a time.Time for [LoadEnv], expecting the time.RFC3339 format.
func TimeParser(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}
