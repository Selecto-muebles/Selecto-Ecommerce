package editorial

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// Camera JPEGs often store rotation in EXIF instead of rotating their pixels.
// Respect it before resizing, without allocating another full-size bitmap.
func posterOrientation(data []byte) uint16 {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for at := 2; at+4 <= len(data); {
		if data[at] != 0xff {
			return 1
		}
		kind := data[at+1]
		if kind == 0xda || kind == 0xd9 {
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[at+2 : at+4]))
		if n < 2 || at+2+n > len(data) {
			return 1
		}
		segment := data[at+4 : at+2+n]
		at += 2 + n
		if kind != 0xe1 || !bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			continue
		}
		tiff := segment[6:]
		if len(tiff) < 8 {
			return 1
		}
		var order binary.ByteOrder = binary.LittleEndian
		if string(tiff[:2]) == "MM" {
			order = binary.BigEndian
		} else if string(tiff[:2]) != "II" {
			return 1
		}
		offset := uint64(order.Uint32(tiff[4:8]))
		if offset+2 > uint64(len(tiff)) {
			return 1
		}
		count := int(order.Uint16(tiff[offset : offset+2]))
		for i := 0; i < count; i++ {
			pos := int(offset) + 2 + i*12
			if pos+12 > len(tiff) {
				return 1
			}
			entry := tiff[pos : pos+12]
			if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
				value := order.Uint16(entry[8:10])
				if value >= 1 && value <= 8 {
					return value
				}
				return 1
			}
		}
	}
	return 1
}

type orientedPoster struct {
	source      image.Image
	orientation uint16
}

func (o orientedPoster) ColorModel() color.Model { return o.source.ColorModel() }
func (o orientedPoster) Bounds() image.Rectangle {
	b := o.source.Bounds()
	if o.orientation >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}
func (o orientedPoster) At(x, y int) color.Color {
	b := o.source.Bounds()
	w, h := b.Dx(), b.Dy()
	switch o.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return o.source.At(b.Min.X+x, b.Min.Y+y)
}
