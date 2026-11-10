package picture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	// Registers the JPEG decoder with the image package.
	_ "image/jpeg"
	// Registers the PNG decoder with the image package.
	_ "image/png"

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
	"png":  "image/png",
	"webp": "image/webp",
}

// Rule is what a picture contract accepts: a still WebP or a JPEG, or a still PNG when AllowPNG is set,
// 1 to MaxWidth pixels wide, of an exact aspect ratio when the rule sets one.
type Rule struct {
	// RatioWidth and RatioHeight set the exact aspect ratio, such as 1:1 or 16:9. Both zero accept any
	// ratio.
	RatioWidth, RatioHeight int
	// MaxWidth caps the width, in pixels.
	MaxWidth int
	// MaxHeight caps the height, in pixels. Zero leaves the height uncapped, so a rule that accepts any
	// ratio sets it to bound the memory [Check] spends decoding.
	MaxHeight int
	// AllowPNG accepts a still PNG beside a WebP or a JPEG. A rule refuses every PNG without it.
	AllowPNG bool
}

// RefusedError names the rule a refused picture broke, and matches [ErrRefused]. Its value is the
// error.type of the span that refuses the picture, and the reason a service answers its client with.
type RefusedError string

const (
	// ErrFormat refuses bytes that are neither a still WebP, a JPEG nor a still PNG the rule allows, or
	// do not decode whole.
	ErrFormat RefusedError = "format"
	// ErrRatio refuses a picture whose aspect ratio differs from the rule's.
	ErrRatio RefusedError = "ratio"
	// ErrDimensions refuses a picture under 1 pixel on a side, wider than [Rule.MaxWidth], or taller than a
	// set [Rule.MaxHeight].
	ErrDimensions RefusedError = "dimensions"
)

func (refusal RefusedError) Error() string {
	return ErrRefused.Error() + ": " + string(refusal)
}

// Is reports the refusal as [ErrRefused].
func (refusal RefusedError) Is(target error) bool {
	return target == ErrRefused
}

// Check returns the media type, "image/webp", "image/jpeg" or "image/png", of a picture rule accepts.
// It returns a [RefusedError] otherwise, joined with the decoder's error when one caused it.
//
// Check reads the dimensions from the header before it decodes the image, so a small file that
// declares dimensions past the rule costs no memory. Its span takes the refusal as its error.type, so
// traces count refusals by reason.
func Check(ctx context.Context, data []byte, rule Rule) (string, error) {
	_, span := otel.Tracer().Start(ctx, "picture.Check")
	defer span.End()

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	mediaType, accepted := mediaTypes[format]

	switch {
	case err != nil, !accepted,
		format == "webp" && webpAnimated(data),
		format == "png" && (!rule.AllowPNG || pngAnimated(data)):
		return "", refuse(span, ErrFormat, err)
	// A rule without a ratio multiplies both sides by zero, so every picture passes.
	case config.Width*rule.RatioHeight != config.Height*rule.RatioWidth:
		return "", refuse(span, ErrRatio, nil)
	case config.Width < 1, config.Width > rule.MaxWidth,
		config.Height < 1, rule.MaxHeight > 0 && config.Height > rule.MaxHeight:
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

// pngAnimated reports whether a PNG declares an animation: an acTL chunk comes before its first IDAT
// (APNG Specification, "Structure"). A chunk holds a 4-byte length, a 4-byte type, its data and a
// 4-byte checksum. The walk stops at a chunk that runs past the data, which the decoder then refuses.
func pngAnimated(data []byte) bool {
	const (
		signatureSize = 8
		headerSize    = 8
		checksumSize  = 4
	)

	if len(data) < signatureSize {
		return false
	}

	for rest := data[signatureSize:]; len(rest) >= headerSize; {
		switch string(rest[4:headerSize]) {
		case "acTL":
			return true
		case "IDAT":
			return false
		}

		size := uint64(binary.BigEndian.Uint32(rest)) + headerSize + checksumSize
		if size > uint64(len(rest)) {
			return false
		}

		rest = rest[size:]
	}

	return false
}
