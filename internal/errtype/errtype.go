package errtype

import "fmt"

// Innermost names the type of the innermost error value wraps, such as "*pgconn.PgError". A wrapped
// error carries no stack trace, so its innermost cause tells faults apart. Through an error joining
// several, it follows the first. A value that wraps nothing, such as a string a panic recovered,
// names its own type.
func Innermost(value any) string {
	for {
		var next error

		switch wrapper := value.(type) {
		case interface{ Unwrap() error }:
			next = wrapper.Unwrap()
		case interface{ Unwrap() []error }:
			if causes := wrapper.Unwrap(); len(causes) > 0 {
				next = causes[0]
			}
		}

		if next == nil {
			return fmt.Sprintf("%T", value)
		}

		value = next
	}
}
