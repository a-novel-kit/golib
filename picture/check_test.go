package picture_test

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"

	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/a-novel-kit/golib/picture"
)

// libwebp 1.6 encoded the WebP fixtures.
var (
	// stillWebP is a 16 px square, encoded with cwebp -q 90.
	//go:embed testdata/still.webp
	stillWebP []byte
	// alphaWebP is a translucent 16 px square, encoded with cwebp -q 90 -exact.
	//go:embed testdata/alpha.webp
	alphaWebP []byte
	// animatedWebP shows two frames of a 16 px square, encoded with img2webp -lossy -q 90.
	//go:embed testdata/animated.webp
	animatedWebP []byte
	// wideWebP is 32x18 px, encoded with cwebp -q 90.
	//go:embed testdata/wide.webp
	wideWebP []byte
)

var (
	// square is the rule of a logo or a profile picture.
	square = picture.Rule{RatioWidth: 1, RatioHeight: 1, MaxWidth: 512}
	// wide is the rule of a 16:9 background.
	wide = picture.Rule{RatioWidth: 16, RatioHeight: 9, MaxWidth: 1280}
)

//nolint:paralleltest // Swaps the process-wide tracer provider to read the span of each check.
func TestCheck(t *testing.T) {
	squareJPEG := newJPEG(t, 64, 64)

	var pngPicture bytes.Buffer

	require.NoError(t, png.Encode(&pngPicture, newImage(64, 64)))

	testCases := []struct {
		name string

		data []byte
		rule picture.Rule

		expect    string
		expectErr error
	}{
		{
			name: "Success/WebP",

			data: stillWebP,
			rule: square,

			expect: "image/webp",
		},
		{
			name: "Success/WebPAlpha",

			data: alphaWebP,
			rule: square,

			expect: "image/webp",
		},
		{
			name: "Success/WebPWide",

			data: wideWebP,
			rule: wide,

			expect: "image/webp",
		},
		{
			name: "Success/JPEG",

			data: squareJPEG,
			rule: square,

			expect: "image/jpeg",
		},
		{
			name: "Success/JPEGWide",

			data: newJPEG(t, 1280, 720),
			rule: wide,

			expect: "image/jpeg",
		},
		{
			name: "Success/MaxWidth",

			data: newJPEG(t, 512, 512),
			rule: square,

			expect: "image/jpeg",
		},
		{
			name: "Success/OnePixel",

			data: newJPEG(t, 1, 1),
			rule: square,

			expect: "image/jpeg",
		},
		{
			name: "Error/NotAnImage",

			data: []byte("not an image"),
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/Empty",

			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/PNG",

			data: pngPicture.Bytes(),
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/AnimatedWebP",

			data: animatedWebP,
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			// The decoder reads the still frame and ignores the flag, so only the flag refuses it.
			name: "Error/AnimationFlag",

			data: withAnimationFlag(alphaWebP),
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/TruncatedJPEG",

			data: squareJPEG[:len(squareJPEG)*3/4],
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/TruncatedWebP",

			data: stillWebP[:len(stillWebP)-8],
			rule: square,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/RatioNotSquare",

			data: newJPEG(t, 64, 32),
			rule: square,

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/RatioNotWide",

			data: squareJPEG,
			rule: wide,

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/RatioNearlyWide",

			data: newJPEG(t, 1280, 719),
			rule: wide,

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/DimensionsOverMaxWidth",

			data: newJPEG(t, 513, 513),
			rule: square,

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsWideOverMaxWidth",

			data: newJPEG(t, 1296, 729),
			rule: wide,

			expectErr: picture.ErrDimensions,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := recordSpans(t)

			mediaType, err := picture.Check(t.Context(), testCase.data, testCase.rule)
			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expect, mediaType)

			// Services read a refusal both ways, and the span names it as its error.type.
			var refusal picture.RefusedError

			require.Equal(t, err != nil, errors.As(err, &refusal))
			require.Equal(t, err != nil, errors.Is(err, picture.ErrRefused))

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			require.Equal(t, err != nil, spans[0].Status().Code == codes.Error)

			attributes := attribute.NewSet(spans[0].Attributes()...)
			errorType, _ := attributes.Value("error.type")
			require.Equal(t, string(refusal), errorType.AsString())
		})
	}
}

// recordSpans installs a tracer provider that records the spans started until the test ends.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otelglobal.GetTracerProvider()

	otelglobal.SetTracerProvider(provider)
	t.Cleanup(func() {
		otelglobal.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.WithoutCancel(t.Context())))
	})

	return recorder
}

// newImage returns an image of the given size, shaded so its encoding holds more than a header.
func newImage(width, height int) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))

	for x := range width {
		for y := range height {
			img.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*13) % 256)})
		}
	}

	return img
}

// withAnimationFlag returns a copy of a WebP whose extended header sets the animation flag.
func withAnimationFlag(webp []byte) []byte {
	flagged := bytes.Clone(webp)
	flagged[20] |= 1 << 1

	return flagged
}

// newJPEG encodes an image of the given size as a JPEG.
func newJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer

	err := jpeg.Encode(&buf, newImage(width, height), nil)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}
