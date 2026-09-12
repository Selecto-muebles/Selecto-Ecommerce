package editorial

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"Selecto-Ecommerce/internal/shared/utils"
)

const MaxImageBytes = 2 << 20
const MaxSlides = 20

var ErrInvalid = errors.New("invalid carousel content")
var ErrConflict = errors.New("carousel changed, limit reached or destination unavailable")
var ErrNotFound = errors.New("carousel slide not found")

type Input struct {
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	AltText    string `json:"alt_text"`
	CTALabel   string `json:"cta_label"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
	SortOrder  int    `json:"sort_order"`
	Active     bool   `json:"active"`
	Version    int    `json:"version"`
}

type Slide struct {
	Input
	ID                   int    `json:"id"`
	DestinationAvailable bool   `json:"destination_available"`
	DestinationName      string `json:"destination_name"`
	Href                 string `json:"href"`
	ImageURL             string `json:"image_url"`
}

type Image struct {
	Content []byte
	MIME    string
}

type Store interface {
	List(context.Context, bool) ([]Slide, error)
	Save(context.Context, int, Input, int, *Image, string) (int, error)
	Delete(context.Context, int, int, string) error
	Image(context.Context, int, bool) (Image, error)
}

func Save(ctx context.Context, store Store, id int, in Input, img *Image, actor string) (int, error) {
	in.Title, in.Subtitle = strings.TrimSpace(in.Title), strings.TrimSpace(in.Subtitle)
	in.AltText, in.CTALabel = strings.TrimSpace(in.AltText), strings.TrimSpace(in.CTALabel)
	if !length(in.Title, 1, 100) || !length(in.Subtitle, 0, 240) || !length(in.AltText, 1, 180) || !length(in.CTALabel, 1, 50) || in.SortOrder < 0 || in.SortOrder > 999 || (id > 0 && in.Version < 1) {
		return 0, ErrInvalid
	}
	var target int
	var err error
	switch in.TargetKind {
	case "product":
		target, err = utils.DecodeID(in.TargetID)
	case "category":
		target, err = strconv.Atoi(in.TargetID)
	default:
		return 0, ErrInvalid
	}
	if err != nil || target <= 0 {
		return 0, ErrInvalid
	}
	if id == 0 && img == nil {
		return 0, ErrInvalid
	}
	if img != nil {
		if len(img.Content) == 0 || len(img.Content) > MaxImageBytes {
			return 0, ErrInvalid
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Content))
		if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			return 0, ErrInvalid
		}
		img.MIME = "image/" + format
	}
	return store.Save(ctx, id, in, target, img, actor)
}

func length(s string, min, max int) bool { n := utf8.RuneCountInString(s); return n >= min && n <= max }

func Destination(kind string, id int, name string) (string, string) {
	if kind == "product" {
		encoded := utils.EncodeID(id)
		return encoded, "/productos/" + encoded
	}
	return strconv.Itoa(id), "/?category=" + url.QueryEscape(name)
}
