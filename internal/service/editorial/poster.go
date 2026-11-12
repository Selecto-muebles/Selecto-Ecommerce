package editorial

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"sync"
)

type Rendition struct {
	Content []byte
	MIME    string
}

var posterTransform sync.Mutex

// Derivatives are generated once, not at every page view. Preserve originals
// and transparency; never enlarge small artwork or replace a smaller original.
func PreparePoster(img *Image) error {
	variants, mime, err := prepareRenditions(img.Content, 960, 1920)
	if err != nil {
		return err
	}
	img.MIME, img.Small, img.Large = mime, variants[0], variants[1]
	return nil
}

// PrepareRendition uses the same bounded, orientation-aware image processing as
// editorial covers. The caller retains the original bytes unchanged.
func PrepareRendition(content []byte, width int) (Rendition, error) {
	if width < 1 || width > 1920 {
		return Rendition{}, ErrInvalid
	}
	variants, _, err := prepareRenditions(content, width)
	if err != nil {
		return Rendition{}, err
	}
	return variants[0], nil
}

func prepareRenditions(content []byte, widths ...int) ([]Rendition, string, error) {
	posterTransform.Lock()
	defer posterTransform.Unlock()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return nil, "", ErrInvalid
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, "", ErrInvalid
	}
	if orientation := posterOrientation(content); orientation != 1 {
		source = orientedPoster{source, orientation}
		cfg.Width, cfg.Height = source.Bounds().Dx(), source.Bounds().Dy()
	}
	mime := "image/" + format
	makeSize := func(maxWidth int) (Rendition, error) {
		w := min(cfg.Width, maxWidth)
		h := max(1, cfg.Height*w/cfg.Width)
		// Bound very tall sources as well as wide ones.
		if h > 1920 {
			w = max(1, w*1920/h)
			h = 1920
		}
		// Re-sampling an already correctly sized photo visits every pixel four
		// times and allocates millions of temporary colors. Encode it directly.
		var dst image.Image = source
		unchanged := w == cfg.Width && h == cfg.Height
		if !unchanged {
			resized := image.NewRGBA(image.Rect(0, 0, w, h))
			resizePoster(resized, source)
			dst = resized
		}
		var jpg bytes.Buffer
		if err := jpeg.Encode(&jpg, dst, &jpeg.Options{Quality: 82}); err != nil {
			return Rendition{}, err
		}
		result := Rendition{jpg.Bytes(), "image/jpeg"}
		if format == "png" {
			// Keep alpha for transparent artwork instead of flattening it.
			pngContent := content
			if !unchanged {
				var pngData bytes.Buffer
				if err := png.Encode(&pngData, dst); err != nil {
					return Rendition{}, err
				}
				pngContent = pngData.Bytes()
			}
			opaque, known := dst.(interface{ Opaque() bool })
			if !known || !opaque.Opaque() || len(pngContent) < len(result.Content) {
				result = Rendition{pngContent, "image/png"}
			}
		}
		if cfg.Width == w && cfg.Height == h && len(content) < len(result.Content) {
			result = Rendition{content, mime}
		}
		return result, nil
	}
	variants := make([]Rendition, 0, len(widths))
	for _, width := range widths {
		variant, err := makeSize(width)
		if err != nil {
			return nil, "", err
		}
		variants = append(variants, variant)
	}
	return variants, mime, nil
}

// Bilinear resampling, bounded by the output dimensions. No external native
// image processors or new runtime dependencies are needed.
func resizePoster(dst *image.RGBA, src image.Image) {
	b := src.Bounds()
	sample := func(x, y int) (uint32, uint32, uint32, uint32) { return src.At(x, y).RGBA() }
	if fast, ok := src.(image.RGBA64Image); ok {
		sample = func(x, y int) (uint32, uint32, uint32, uint32) {
			pixel := fast.RGBA64At(x, y)
			return uint32(pixel.R), uint32(pixel.G), uint32(pixel.B), uint32(pixel.A)
		}
	}
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	for y := 0; y < dh; y++ {
		sy := (float64(y)+.5)*float64(b.Dy())/float64(dh) - .5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		y0 = max(0, min(y0, b.Dy()-1))
		y1 := min(y0+1, b.Dy()-1)
		for x := 0; x < dw; x++ {
			sx := (float64(x)+.5)*float64(b.Dx())/float64(dw) - .5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			x0 = max(0, min(x0, b.Dx()-1))
			x1 := min(x0+1, b.Dx()-1)
			r0, g0, b0, a0 := sample(b.Min.X+x0, b.Min.Y+y0)
			r1, g1, b1, a1 := sample(b.Min.X+x1, b.Min.Y+y0)
			r2, g2, b2, a2 := sample(b.Min.X+x0, b.Min.Y+y1)
			r3, g3, b3, a3 := sample(b.Min.X+x1, b.Min.Y+y1)
			mix := func(a, b, c, d uint32) uint8 {
				return uint8(((float64(a)*(1-fx)+float64(b)*fx)*(1-fy) + (float64(c)*(1-fx)+float64(d)*fx)*fy) / 257)
			}
			dst.SetRGBA(x, y, color.RGBA{mix(r0, r1, r2, r3), mix(g0, g1, g2, g3), mix(b0, b1, b2, b3), mix(a0, a1, a2, a3)})
		}
	}
}
