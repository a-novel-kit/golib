package picture_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"slices"
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
	// square is the rule of a profile picture.
	square = picture.Rule{RatioWidth: 1, RatioHeight: 1, MaxWidth: 512}
	// wide is the rule of a 16:9 background.
	wide = picture.Rule{RatioWidth: 16, RatioHeight: 9, MaxWidth: 1280}
	// free is the rule of a logo of any aspect ratio, capped on both sides.
	free = picture.Rule{MaxWidth: 1024, MaxHeight: 128}
	// freePNG is free, opened to PNG.
	freePNG = picture.Rule{MaxWidth: 1024, MaxHeight: 128, AllowPNG: true}
)

//nolint:paralleltest // Swaps the process-wide tracer provider to read the span of each check.
func TestCheck(t *testing.T) {
	squareJPEG := newJPEG(t, 64, 64)

	var pngPicture bytes.Buffer

	require.NoError(t, png.Encode(&pngPicture, newImage(64, 64)))

	actl := animationControlChunk()

	// cappedJPEG is the picture the byte cap cases measure.
	cappedJPEG := newJPEG(t, 32, 32)

	// combined sets every option at once, each at the size of cappedJPEG.
	combined := picture.Rule{
		RatioWidth: 1, RatioHeight: 1, MaxWidth: 32, MaxHeight: 32, MaxBytes: len(cappedJPEG),
	}

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
			name: "Success/FreeRatioWidest",

			data: newJPEG(t, 1024, 128),
			rule: free,

			expect: "image/jpeg",
		},
		{
			name: "Success/FreeRatioTallest",

			data: newJPEG(t, 32, 128),
			rule: free,

			expect: "image/jpeg",
		},
		{
			name: "Success/FreeRatioWebP",

			data: wideWebP,
			rule: free,

			expect: "image/webp",
		},
		{
			name: "Success/FreeRatioOnePixel",

			data: newJPEG(t, 1, 1),
			rule: free,

			expect: "image/jpeg",
		},
		{
			name: "Success/MaxWidthAlone",

			data: newJPEG(t, 16, 2048),
			rule: picture.Rule{MaxWidth: 16},

			expect: "image/jpeg",
		},
		{
			name: "Success/NoOption",

			data: newJPEG(t, 2048, 16),
			rule: picture.Rule{},

			expect: "image/jpeg",
		},
		{
			name: "Success/RatioAlone",

			data: newJPEG(t, 2048, 1024),
			rule: picture.Rule{RatioWidth: 2, RatioHeight: 1},

			expect: "image/jpeg",
		},
		{
			name: "Success/MaxHeightAlone",

			data: newJPEG(t, 2048, 64),
			rule: picture.Rule{MaxHeight: 64},

			expect: "image/jpeg",
		},
		{
			name: "Success/MaxBytesAtCap",

			data: cappedJPEG,
			rule: picture.Rule{MaxBytes: len(cappedJPEG)},

			expect: "image/jpeg",
		},
		{
			name: "Success/Combination",

			data: cappedJPEG,
			rule: combined,

			expect: "image/jpeg",
		},
		{
			name: "Success/PNG",

			data: pngPicture.Bytes(),
			rule: freePNG,

			expect: "image/png",
		},
		{
			// The APNG specification reads acTL only before the first IDAT, so this PNG stays still.
			name: "Success/PNGAnimationControlAfterData",

			data: withChunkBefore(pngPicture.Bytes(), "IEND", actl),
			rule: freePNG,

			expect: "image/png",
		},
		{
			name: "Error/PNGNotAllowed",

			data: pngPicture.Bytes(),
			rule: free,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/AnimatedPNG",

			data: withChunkBefore(pngPicture.Bytes(), "IDAT", actl),
			rule: freePNG,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/TruncatedPNG",

			data: pngPicture.Bytes()[:pngPicture.Len()/2],
			rule: freePNG,

			expectErr: picture.ErrFormat,
		},
		{
			// The chunk walk stops at a length past the data, and the decoder refuses the bytes.
			name: "Error/PNGChunkPastTheData",

			data: withChunkBefore(pngPicture.Bytes(), "IDAT", []byte("\xff\xff\xff\xffjunk")),
			rule: freePNG,

			expectErr: picture.ErrFormat,
		},
		{
			name: "Error/DimensionsPNGOverMaxHeight",

			data: newPNG(t, 64, 129),
			rule: freePNG,

			expectErr: picture.ErrDimensions,
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
		{
			name: "Error/DimensionsFreeOverMaxWidth",

			data: newJPEG(t, 1025, 128),
			rule: free,

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsFreeOverMaxHeight",

			data: newJPEG(t, 64, 129),
			rule: free,

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsRatioOverMaxHeight",

			data: newJPEG(t, 65, 65),
			rule: picture.Rule{RatioWidth: 1, RatioHeight: 1, MaxWidth: 512, MaxHeight: 64},

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/RatioAlone",

			data: newJPEG(t, 64, 33),
			rule: picture.Rule{RatioWidth: 2, RatioHeight: 1},

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/RatioWidthOnly",

			data: squareJPEG,
			rule: picture.Rule{RatioWidth: 16},

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/RatioHeightOnly",

			data: squareJPEG,
			rule: picture.Rule{RatioHeight: 9},

			expectErr: picture.ErrRatio,
		},
		{
			name: "Error/DimensionsMaxWidthAlone",

			data: newJPEG(t, 65, 16),
			rule: picture.Rule{MaxWidth: 64},

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsMaxHeightAlone",

			data: newJPEG(t, 16, 65),
			rule: picture.Rule{MaxHeight: 64},

			expectErr: picture.ErrDimensions,
		},
		{
			// A negative cap fails closed.
			name: "Error/DimensionsNegativeCap",

			data: squareJPEG,
			rule: picture.Rule{MaxWidth: -1},

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsCombination",

			data: squareJPEG,
			rule: picture.Rule{RatioWidth: 1, RatioHeight: 1, MaxWidth: 32, MaxHeight: 32, MaxBytes: len(squareJPEG)},

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/SizePastCap",

			data: cappedJPEG,
			rule: picture.Rule{MaxBytes: len(cappedJPEG) - 1},

			expectErr: picture.ErrSize,
		},
		{
			// The size comes first, so bytes that do not decode name the size.
			name: "Error/SizeBeforeFormat",

			data: []byte("not an image"),
			rule: picture.Rule{MaxBytes: 4},

			expectErr: picture.ErrSize,
		},
		{
			// The header carries no image data, so a refusal that names the dimensions never decoded it.
			name: "Error/DimensionsPastCeiling",

			data: newPNGHeader(65536, 65536),
			rule: picture.Rule{AllowPNG: true},

			expectErr: picture.ErrDimensions,
		},
		{
			name: "Error/DimensionsPastCeilingWithinCaps",

			data: newPNGHeader(4097, 4096),
			rule: picture.Rule{MaxWidth: 1 << 20, MaxHeight: 1 << 20, AllowPNG: true},

			expectErr: picture.ErrDimensions,
		},
		{
			// At the ceiling the check reaches the decoder, which finds no image data.
			name: "Error/FormatAtCeiling",

			data: newPNGHeader(4096, 4096),
			rule: picture.Rule{AllowPNG: true},

			expectErr: picture.ErrFormat,
		},
		{
			// Without a ratio to refuse it, only the height check stops a header that declares no rows.
			name: "Error/DimensionsNoHeight",

			data: withJPEGHeight(squareJPEG, 0),
			rule: free,

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

// withJPEGHeight returns a copy of a JPEG whose frame header declares the given height.
func withJPEGHeight(jpg []byte, height uint16) []byte {
	const startOfFrame = "\xff\xc0"

	patched := bytes.Clone(jpg)
	offset := bytes.Index(patched, []byte(startOfFrame))

	// The marker's length, then the sample precision, come before the height.
	binary.BigEndian.PutUint16(patched[offset+5:], height)

	return patched
}

// animationControlChunk returns an acTL chunk of a PNG, with its checksum: one frame, played once.
func animationControlChunk() []byte {
	chunk := []byte("\x00\x00\x00\x08acTL\x00\x00\x00\x01\x00\x00\x00\x01")

	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

// withChunkBefore returns a copy of a PNG with chunk inserted before the first chunk of the given type.
func withChunkBefore(encoded []byte, chunkType string, chunk []byte) []byte {
	// A chunk's 4-byte length comes before its type.
	offset := bytes.Index(encoded, []byte(chunkType)) - 4

	return slices.Concat(encoded[:offset], chunk, encoded[offset:])
}

// newPNGHeader returns a PNG that ends after its IHDR chunk, which declares the given size in 8-bit RGBA.
func newPNGHeader(width, height uint32) []byte {
	header := []byte("\x00\x00\x00\x0dIHDR")
	header = binary.BigEndian.AppendUint32(header, width)
	header = binary.BigEndian.AppendUint32(header, height)
	header = append(header, 8, 6, 0, 0, 0)
	header = binary.BigEndian.AppendUint32(header, crc32.ChecksumIEEE(header[4:]))

	return slices.Concat([]byte("\x89PNG\r\n\x1a\n"), header)
}

// newPNG encodes an image of the given size as a PNG.
func newPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer

	err := png.Encode(&buf, newImage(width, height))
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
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
