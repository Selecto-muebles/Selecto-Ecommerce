package catalog

import (
	"encoding/binary"
	"errors"
	"net/http"
)

const MaxVideoBytes = 20 << 20
const MaxVideoSeconds = 60

var ErrVideo = errors.New("El video debe ser MP4 con video H.264 y audio AAC opcional, de hasta 60 segundos y 20 MB")

type mp4Box struct {
	kind string
	data []byte
}

// Read only bounded ISO BMFF boxes. A filename or a MIME supplied by the client
// is not proof of a playable MP4. Encrypted and unsupported codecs are rejected.
func mp4Boxes(data []byte) ([]mp4Box, bool) {
	var boxes []mp4Box
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, false
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), 8
		kind := string(data[4:8])
		if size == 1 {
			if len(data) < 16 {
				return nil, false
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		} else if size == 0 {
			size = uint64(len(data))
		}
		if size < uint64(header) || size > uint64(len(data)) || len(boxes) >= 4096 {
			return nil, false
		}
		boxes = append(boxes, mp4Box{kind, data[header:int(size)]})
		data = data[int(size):]
	}
	return boxes, true
}

func childBox(data []byte, kind string) ([]byte, bool) {
	boxes, ok := mp4Boxes(data)
	if ok {
		for _, box := range boxes {
			if box.kind == kind {
				return box.data, true
			}
		}
	}
	return nil, false
}

func ValidateProductVideo(data []byte) error {
	if len(data) == 0 || len(data) > MaxVideoBytes || http.DetectContentType(data) != "video/mp4" {
		return ErrVideo
	}
	mdat, ok := childBox(data, "mdat")
	if !ok || len(mdat) == 0 {
		return ErrVideo
	}
	moov, ok := childBox(data, "moov")
	if !ok {
		return ErrVideo
	}
	mvhd, ok := childBox(moov, "mvhd")
	if !ok || !validVideoDuration(mvhd) {
		return ErrVideo
	}
	tracks, ok := mp4Boxes(moov)
	if !ok {
		return ErrVideo
	}
	video := false
	for _, track := range tracks {
		if track.kind != "trak" {
			continue
		}
		mdia, ok := childBox(track.data, "mdia")
		if !ok {
			return ErrVideo
		}
		hdlr, ok := childBox(mdia, "hdlr")
		if !ok || len(hdlr) < 12 {
			return ErrVideo
		}
		kind := string(hdlr[8:12])
		if kind != "vide" && kind != "soun" {
			continue
		}
		minf, _ := childBox(mdia, "minf")
		stbl, _ := childBox(minf, "stbl")
		stsd, ok := childBox(stbl, "stsd")
		if !ok || len(stsd) < 8 || binary.BigEndian.Uint32(stsd[4:8]) != 1 {
			return ErrVideo
		}
		entries, ok := mp4Boxes(stsd[8:])
		if !ok || len(entries) != 1 {
			return ErrVideo
		}
		entry := entries[0]
		if kind == "soun" {
			if entry.kind != "mp4a" {
				return ErrVideo
			}
			continue
		}
		if (entry.kind != "avc1" && entry.kind != "avc3") || len(entry.data) < 78 {
			return ErrVideo
		}
		width, height := binary.BigEndian.Uint16(entry.data[24:26]), binary.BigEndian.Uint16(entry.data[26:28])
		avc, ok := childBox(entry.data[78:], "avcC")
		if width == 0 || height == 0 || width > 4096 || height > 4096 || !ok || len(avc) < 7 || avc[0] != 1 {
			return ErrVideo
		}
		video = true
	}
	if !video {
		return ErrVideo
	}
	return nil
}

func validVideoDuration(mvhd []byte) bool {
	var scale uint32
	var duration uint64
	if len(mvhd) >= 20 && mvhd[0] == 0 {
		scale, duration = binary.BigEndian.Uint32(mvhd[12:16]), uint64(binary.BigEndian.Uint32(mvhd[16:20]))
	} else if len(mvhd) >= 32 && mvhd[0] == 1 {
		scale, duration = binary.BigEndian.Uint32(mvhd[20:24]), binary.BigEndian.Uint64(mvhd[24:32])
	}
	return scale > 0 && duration > 0 && duration <= uint64(scale)*MaxVideoSeconds
}
