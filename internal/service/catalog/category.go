package catalog

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrInvalidCategory = errors.New("category name and slug must be valid")
var categorySlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
}

func NormalizeCategory(name, slug string) (Category, error) {
	name, slug = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(slug))
	if name == "" || utf8.RuneCountInString(name) > 80 || len(slug) > 100 || !categorySlug.MatchString(slug) {
		return Category{}, ErrInvalidCategory
	}
	return Category{Name: name, Slug: slug, Active: true}, nil
}
