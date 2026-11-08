package picture

import (
	"bytes"
	"context"
	"errors"
	"image"
	// Registers the JPEG decoder with the image package.
	_ "image/jpeg"

	// Registers the WebP decoder with the image package.
	_ "golang.org/x/image/webp"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// ErrRefused is returned by [Check], as a [RefusedError], for a picture its rule refuses.
var ErrRefused = errors.New("picture refused")

// mediaTypes maps each format a rule accepts, as the image package names it, to its media type. The
// bytes name the format; a request's Content-Type never does.
var mediaTypes = map[string]string{
	"jpeg": "image/jpeg",
	"webp": "image/webp",
}

// Rule is what a picture contract accepts: a still WebP or a JPEG of an exact aspect ratio, 1 to
// MaxWidth pixels wide.
type Rule struct {
	// RatioWidth and RatioHeight set the exact aspect ratio, such as 1:1 or 16:9.
	RatioWidth, RatioHeight int
	// MaxWidth caps the width, in pixels.
	MaxWidth int
}

// RefusedError names the rule a refused picture broke, and matches [ErrRefused]. Its value is the
// error.type of the span that refuses the picture, and the reason a service answers its client with.
type RefusedError string

const (
	// ErrFormat refuses bytes that are neither a still WebP nor a JPEG, or do not decode whole.
	ErrFormat RefusedError = "format"
	// ErrRatio refuses a picture whose aspect ratio differs from the rule's.
	ErrRatio RefusedError = "ratio"
	// ErrDimensions refuses a picture whose width is not 1 to [Rule.MaxWidth] pixels.
	ErrDimensions RefusedError = "dimensions"
)

func (refusal RefusedError) Error() string {
	return ErrRefused.Error() + ": " + string(refusal)
}

// Is reports the refusal as [ErrRefused].
func (refusal RefusedError) Is(target error) bool {
	return target == ErrRefused
}

// Check returns the media type, "image/webp" or "image/jpeg", of a picture rule accepts. It returns a
// [RefusedError] otherwise, joined with the decoder's error when one caused it.
//
// Check reads the dimensions from the header before it decodes the image, so a small file that
// declares a huge image costs no memory. Its span takes the refusal as its error.type, so traces count
// refusals by reason.
func Check(ctx context.Context, data []byte, rule Rule) (string, error) {
	_, span := otel.Tracer().Start(ctx, "picture.Check")
	defer span.End()

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	mediaType, accepted := mediaTypes[format]

	switch {
	case err != nil || !accepted || (format == "webp" && webpAnimated(data)):
		return "", refuse(span, ErrFormat, err)
	case config.Width*rule.RatioHeight != config.Height*rule.RatioWidth:
		return "", refuse(span, ErrRatio, nil)
	case config.Width < 1 || config.Width > rule.MaxWidth:
		return "", refuse(span, ErrDimensions, nil)
	}

	_, _, err = image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", refuse(span, ErrFormat, err)
	}

	return mediaType, nil
}

// refuse marks span failed by refusal, joined with the decoder's error when one caused it, and names
// the refusal as the span's error.type.
func refuse(span trace.Span, refusal RefusedError, cause error) error {
	span.SetAttributes(semconv.ErrorTypeKey.String(string(refusal)))

	return otel.ReportError(span, errors.Join(refusal, cause))
}

// webpAnimated reports whether a WebP declares an animation: its VP8X chunk, which follows the RIFF
// header, sets the animation flag (WebP Container Specification, "Extended File Format").
func webpAnimated(data []byte) bool {
	const animationFlag = 1 << 1

	return len(data) > 20 && string(data[12:16]) == "VP8X" && data[20]&animationFlag != 0
}
