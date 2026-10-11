package httpf

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidRate is returned by [ParseRate] for a value not written "<requests>/<window>".
var ErrInvalidRate = errors.New("invalid rate")

// Rate is a number of requests allowed per window, written "<requests>/<window>", such as "3/5m".
type Rate struct {
	Requests int           `json:"requests" yaml:"requests"`
	Window   time.Duration `json:"window"   yaml:"window"`
}

// String writes the rate the way [ParseRate] reads it.
func (rate Rate) String() string {
	return strconv.Itoa(rate.Requests) + "/" + rate.Window.String()
}

// ParseRate reads "<requests>/<window>", such as "5/1s" or "3/5m", the window being a
// [time.Duration]. Both must be positive. It fits [config.LoadEnv] as a parser.
func ParseRate(value string) (Rate, error) {
	requests, window, found := strings.Cut(value, "/")
	if !found {
		return Rate{}, fmt.Errorf("%w %q: want <requests>/<window>", ErrInvalidRate, value)
	}

	count, err := strconv.Atoi(requests)
	if err != nil || count <= 0 {
		return Rate{}, fmt.Errorf("%w %q: requests must be a positive integer", ErrInvalidRate, value)
	}

	duration, err := time.ParseDuration(window)
	if err != nil || duration <= 0 {
		return Rate{}, fmt.Errorf("%w %q: window must be a positive duration", ErrInvalidRate, value)
	}

	return Rate{Requests: count, Window: duration}, nil
}
