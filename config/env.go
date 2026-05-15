package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LoadEnv cast an environment variable to a native Go type, using the provided Parser function.
// If parsing fails, the fallback value is returned.
func LoadEnv[T any](value string, fallback T, parser func(string) (T, error)) T {
	if value == "" {
		return fallback
	}

	parsedValue, err := parser(value)
	if err != nil {
		return fallback
	}

	return parsedValue
}

// SliceParser is a parsing function for LoadEnv, that returns a slice of values of type T.
// The parser accepts a sub-parser to define the type of T.
func SliceParser[T any](parser func(string) (T, error)) func(string) ([]T, error) {
	return func(value string) ([]T, error) {
		// To force an empty slice if the value is empty.
		if value == "[]" {
			return nil, nil
		}

		// Split source on commas.
		parts := strings.Split(value, ",")

		// Parse each part using the provided parser.
		parsedValues := make([]T, 0, len(parts))

		for _, part := range parts {
			trimmedPart := strings.TrimSpace(part)
			if trimmedPart == "" {
				continue // Skip empty parts.
			}

			parsedValue, err := parser(trimmedPart)
			if err != nil {
				// If any parsing fails, deem the whole slice invalid and return the fallback.
				return nil, err
			}

			parsedValues = append(parsedValues, parsedValue)
		}

		// If no parts were parsed, return the fallback.
		if len(parsedValues) == 0 {
			return nil, fmt.Errorf(`value "%s" is empty`, value)
		}

		return parsedValues, nil
	}
}

// StringParser is a parsing function for LoadEnv, that returns the string content of the variable, as-is.
func StringParser(value string) (string, error) {
	return value, nil
}

func EnumParser[T comparable](parser func(string) (T, error), allow ...T) func(string) (T, error) {
	return func(value string) (T, error) {
		raw, err := parser(value)
		if err != nil {
			return raw, err
		}

		for _, allowed := range allow {
			if raw == allowed {
				return raw, nil
			}
		}

		return raw, fmt.Errorf(`value "%s" is not allowed`, value)
	}
}

// Int64Parser is a parsing function for LoadEnv, that returns the int64 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseInt.
func Int64Parser(value string) (int64, error) {
	return strconv.ParseInt(value, 0, 64)
}

// Int32Parser is a parsing function for LoadEnv, that returns the int32 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseInt.
func Int32Parser(value string) (int32, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 32)

	return int32(parsedValue), err
}

// Int16Parser is a parsing function for LoadEnv, that returns the int16 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseInt.
func Int16Parser(value string) (int16, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 16)

	return int16(parsedValue), err
}

// Int8Parser is a parsing function for LoadEnv, that returns the int8 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseInt.
func Int8Parser(value string) (int8, error) {
	parsedValue, err := strconv.ParseInt(value, 0, 8)

	return int8(parsedValue), err
}

// IntParser is a parsing function for LoadEnv, that returns the int representation of the variable.
// For more information about supported value, refer to the documentation of strconv.Atoi.
func IntParser(value string) (int, error) {
	return strconv.Atoi(value)
}

// Uint64Parser is a parsing function for LoadEnv, that returns the uint64 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseUint.
func Uint64Parser(value string) (uint64, error) {
	return strconv.ParseUint(value, 0, 64)
}

// Uint32Parser is a parsing function for LoadEnv, that returns the uint32 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseUint.
func Uint32Parser(value string) (uint32, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 32)

	return uint32(parsedValue), err
}

// Uint16Parser is a parsing function for LoadEnv, that returns the uint16 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseUint.
func Uint16Parser(value string) (uint16, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 16)

	return uint16(parsedValue), err
}

// Uint8Parser is a parsing function for LoadEnv, that returns the uint8 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseUint.
func Uint8Parser(value string) (uint8, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 8)

	return uint8(parsedValue), err
}

// UintParser is a parsing function for LoadEnv, that returns the uint representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseUint.
func UintParser(value string) (uint, error) {
	parsedValue, err := strconv.ParseUint(value, 0, 0)

	return uint(parsedValue), err
}

// BoolParser is a parsing function for LoadEnv, that returns the boolean equivalent of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseBool.
func BoolParser(value string) (bool, error) {
	return strconv.ParseBool(value)
}

// Float64Parser is a parsing function for LoadEnv, that returns the float64 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseFloat.
func Float64Parser(value string) (float64, error) {
	return strconv.ParseFloat(value, 64)
}

// Float32Parser is a parsing function for LoadEnv, that returns the float32 representation of the variable.
// For more information about supported value, refer to the documentation of strconv.ParseFloat.
func Float32Parser(value string) (float32, error) {
	parsedValue, err := strconv.ParseFloat(value, 32)

	return float32(parsedValue), err
}

// DurationParser is a parsing function for LoadEnv, that returns the time.Duration representation of the variable.
// For more information about supported value, refer to the documentation of time.ParseDuration.
func DurationParser(value string) (time.Duration, error) {
	return time.ParseDuration(value)
}

// TimeParser is a parsing function for LoadEnv, that returns the time.Time representation of the variable.
// The parser expects the time to be in time.RFC3339 format. For more information about supported value,
// refer to the documentation of time.Parse.
func TimeParser(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, value)
}

// JSONMapParser is a parsing function for LoadEnv, that returns the map[string]any representation of the variable.
// The parser expects the value to be a valid JSON object.
func JSONMapParser(value string) (map[string]any, error) {
	var parsedValue map[string]any

	err := json.Unmarshal([]byte(value), &parsedValue)
	if err != nil {
		return nil, err
	}

	return parsedValue, nil
}

// JSONSliceParser is a parsing function for LoadEnv, that returns the []any representation of the variable.
// The parser expects the value to be a valid JSON array.
func JSONSliceParser(value string) ([]any, error) {
	var parsedValue []any

	err := json.Unmarshal([]byte(value), &parsedValue)
	if err != nil {
		return nil, err
	}

	return parsedValue, nil
}
