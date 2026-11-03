package httpf

import (
	"errors"
	"net/http"
	"strconv"
	"time"
)

// retryAfterError is an error [WithRetryAfter] attached a delay to.
type retryAfterError struct {
	error

	delay time.Duration
}

func (err *retryAfterError) Unwrap() error {
	return err.error
}

// RetryAfter returns the delay [WithRetryAfter] attached.
func (err *retryAfterError) RetryAfter() time.Duration {
	return err.delay
}

// WithRetryAfter attaches delay to err, the time after which the refused request may run again.
// When HandleError answers err with 429 Too Many Requests or 503 Service Unavailable, it sends delay
// as the Retry-After header of RFC 9110 §10.2.3, in seconds rounded up. Under any other status, the
// delay is left out.
//
// Call it in the handler, on the error it is about to pass to HandleError. An error that knows its
// own delay needs no wrapping: HandleError reads the delay of the first error in the chain with a
// method RetryAfter() time.Duration.
func WithRetryAfter(err error, delay time.Duration) error {
	if err == nil {
		return nil
	}

	return &retryAfterError{error: err, delay: delay}
}

// setRetryAfter sends the delay err names as Retry-After, when status is one the header goes with.
func setRetryAfter(w http.ResponseWriter, err error, status int) {
	if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
		return
	}

	var delayed interface{ RetryAfter() time.Duration }
	if !errors.As(err, &delayed) {
		return
	}

	// Retry-After counts whole seconds, so a partial one rounds up rather than invite a retry too
	// early.
	seconds := (max(delayed.RetryAfter(), 0) + time.Second - 1) / time.Second
	w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
}
