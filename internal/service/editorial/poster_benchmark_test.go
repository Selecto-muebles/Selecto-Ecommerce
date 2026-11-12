package editorial

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"testing"
)

func BenchmarkProductPhotoRendition(b *testing.B) {
	source := image.NewNRGBA(image.Rect(0, 0, 1254, 1254))
	random := rand.New(rand.NewPCG(7, 11))
	for y := 0; y < 1254; y++ {
		for x := 0; x < 1254; x++ {
			source.SetNRGBA(x, y, color.NRGBA{uint8(random.IntN(256)), uint8(random.IntN(256)), uint8(random.IntN(256)), 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		b.Fatal(err)
	}
	for _, size := range []struct {
		name  string
		width int
	}{{"full", 1280}, {"thumbnail", 240}} {
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := PrepareRendition(original.Bytes(), size.width); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
