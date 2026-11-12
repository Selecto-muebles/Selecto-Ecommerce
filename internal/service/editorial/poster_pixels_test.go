package editorial

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestUnscaledTransparentPNGPreservesAllPixelBits(t *testing.T) {
	source := image.NewNRGBA64(image.Rect(0, 0, 9, 7))
	source.SetNRGBA64(3, 4, color.NRGBA64{R: 53211, G: 17421, B: 62001, A: 32751})
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	rendered, err := PrepareRendition(original.Bytes(), 1280)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.MIME != "image/png" || !bytes.Equal(rendered.Content, original.Bytes()) {
		t.Fatal("unscaled transparent artwork was re-encoded or lost pixel precision")
	}
}

func TestPosterResamplingPreservesColorAndPremultipliedAlpha(t *testing.T) {
	for _, alpha := range []uint8{128, 255} {
		source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: alpha})
		source.SetNRGBA(1, 0, color.NRGBA{G: 255, A: alpha})
		source.SetNRGBA(0, 1, color.NRGBA{B: 255, A: alpha})
		source.SetNRGBA(1, 1, color.NRGBA{A: alpha})
		result := image.NewRGBA(image.Rect(0, 0, 1, 1))
		resizePoster(result, source)
		if got, want := result.RGBAAt(0, 0), (color.RGBA{R: alpha / 4, G: alpha / 4, B: alpha / 4, A: alpha}); got != want {
			t.Fatalf("alpha=%d got %+v want %+v", alpha, got, want)
		}
	}
}
