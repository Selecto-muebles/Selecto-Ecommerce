package catalog

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"testing"
)

//go:embed testdata/clip.mp4
var testClip []byte

func TestProductVideoValidation(t *testing.T) {
	if err := ValidateProductVideo(testClip); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"empty", nil}, {"fake extension", []byte("not an MP4")},
		{"truncated", testClip[:40]}, {"missing data", bytes.Replace(testClip, []byte("mdat"), []byte("junk"), 1)},
		{"unsupported codec", bytes.Replace(testClip, []byte("avc1"), []byte("hvc1"), -1)},
		{"oversized", make([]byte, MaxVideoBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if ValidateProductVideo(test.data) == nil {
				t.Fatal("accepted invalid video")
			}
		})
	}
	long := bytes.Clone(testClip)
	index := bytes.Index(long, []byte("mvhd")) + 4
	scale := binary.BigEndian.Uint32(long[index+12 : index+16])
	binary.BigEndian.PutUint32(long[index+16:index+20], scale*61)
	if ValidateProductVideo(long) == nil {
		t.Fatal("accepted 61 seconds")
	}
}

func FuzzProductVideoValidation(f *testing.F) {
	f.Add(testClip)
	f.Add([]byte{})
	f.Add([]byte("ftyp"))
	f.Fuzz(func(t *testing.T, data []byte) { _ = ValidateProductVideo(data) })
}
