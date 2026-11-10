package editorial

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"testing"
)

func TestPosterRenditionsReduceBytesAndPreserveOriginal(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2400, 1000))
	r := rand.New(rand.NewPCG(7, 17))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 2400; x++ {
			source.SetRGBA(x, y, color.RGBA{uint8(r.IntN(256)), uint8(r.IntN(256)), uint8(r.IntN(256)), 255})
		}
	}
	var original bytes.Buffer
	_ = jpeg.Encode(&original, source, &jpeg.Options{Quality: 85})
	img := &Image{Content: bytes.Clone(original.Bytes())}
	if err := PreparePoster(img); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img.Content, original.Bytes()) {
		t.Fatal("original was replaced")
	}
	for _, size := range []struct {
		r    Rendition
		w, h int
	}{{img.Small, 960, 400}, {img.Large, 1920, 800}} {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(size.r.Content))
		if err != nil || cfg.Width != size.w || cfg.Height != size.h {
			t.Fatalf("dimensions %+v %v", cfg, err)
		}
		if len(size.r.Content) >= original.Len() {
			t.Fatal("rendition is not smaller")
		}
	}
	t.Logf("original=%d small=%d large=%d", original.Len(), len(img.Small.Content), len(img.Large.Content))
}

func TestPosterKeepsTransparencyAndNeverEnlargesArtwork(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 50, 30))
	source.SetNRGBA(10, 10, color.NRGBA{255, 0, 0, 100})
	var buf bytes.Buffer
	_ = png.Encode(&buf, source)
	img := &Image{Content: buf.Bytes()}
	if err := PreparePoster(img); err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(img.Small.Content))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 50 || decoded.Bounds().Dy() != 30 || img.Small.MIME != "image/png" {
		t.Fatal("artwork changed size or format")
	}
	_, _, _, a := decoded.At(0, 0).RGBA()
	if a != 0 {
		t.Fatal("transparent background was flattened")
	}
}

func TestPosterCameraOrientation(t *testing.T) {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil)
	tiff := make([]byte, 26)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], 1)
	binary.LittleEndian.PutUint16(tiff[10:], 0x112)
	binary.LittleEndian.PutUint16(tiff[12:], 3)
	binary.LittleEndian.PutUint32(tiff[14:], 1)
	binary.LittleEndian.PutUint16(tiff[18:], 6)
	exif := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xff, 0xe1, 0, byte(len(exif) + 2)}
	segment = append(segment, exif...)
	data := append(bytes.Clone(buf.Bytes()[:2]), segment...)
	data = append(data, buf.Bytes()[2:]...)
	if posterOrientation(data) != 6 {
		t.Fatal("camera orientation not detected")
	}
	img := &Image{Content: data}
	if err := PreparePoster(img); err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(img.Small.Content))
	if cfg.Width != 20 || cfg.Height != 40 {
		t.Fatalf("wrong oriented dimensions: %+v", cfg)
	}
}
