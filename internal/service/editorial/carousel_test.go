package editorial

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"
)

type captureStore struct {
	Store
	called bool
}

func (s *captureStore) Save(_ context.Context, _ int, _ Input, _ int, _ *Image, _ string) (int, error) {
	s.called = true
	return 1, nil
}

func TestCarouselValidation(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	valid := Input{Title: " Portada ", AltText: "Imagen real", CTALabel: "Ver catálogo", TargetKind: "category", TargetID: "1"}
	s := &captureStore{}
	img := &Image{Content: buf.Bytes()}
	if _, err := Save(context.Background(), s, 0, valid, img, "admin"); err != nil || !s.called || img.MIME != "image/png" {
		t.Fatalf("valid: %v", err)
	}
	for _, mutate := range []func(*Input){
		func(i *Input) { i.Title = " " }, func(i *Input) { i.TargetKind = "url" }, func(i *Input) { i.TargetID = "https://evil.test" },
		func(i *Input) { i.SortOrder = -1 }, func(i *Input) { i.AltText = "" }, func(i *Input) { i.TargetID = "0" },
	} {
		in := valid
		mutate(&in)
		s.called = false
		if _, err := Save(context.Background(), s, 0, in, img, "admin"); !errors.Is(err, ErrInvalid) || s.called {
			t.Fatalf("accepted %+v", in)
		}
	}
	for _, bad := range []*Image{nil, {Content: []byte("<svg/>")}, {Content: make([]byte, MaxImageBytes+1)}} {
		if _, err := Save(context.Background(), s, 0, valid, bad, "admin"); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted invalid image")
		}
	}
	if _, err := Save(context.Background(), s, 1, valid, nil, "admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing version accepted")
	}
	valid.Version = 1
	if _, err := Save(context.Background(), s, 1, valid, nil, "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestDestinationIsInternalAndEscaped(t *testing.T) {
	_, href := Destination("category", 1, "Barras & discos")
	if href != "/?category=Barras+%26+discos" {
		t.Fatal(href)
	}
}
