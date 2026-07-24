package catalog

import (
	"errors"
	"strings"

	"Selecto-Ecommerce/internal/shared/collection"
)

type Option struct {
	Name      string   `json:"name"`
	Values    []string `json:"values"`
	SortOrder int      `json:"sort_order"`
}

func NormalizeOptions(options []Option) ([]Option, error) {
	if len(options) > 5 {
		return nil, errors.New("a product can contain at most 5 options")
	}
	seenNames := make(map[string]struct{}, len(options))
	result := make([]Option, 0, len(options))
	for index, option := range options {
		name := strings.TrimSpace(option.Name)
		if name == "" || len(name) > 60 {
			return nil, errors.New("option name must be valid")
		}
		key := strings.ToLower(name)
		if _, exists := seenNames[key]; exists {
			return nil, errors.New("option names must be unique")
		}
		seenNames[key] = struct{}{}

		values, err := normalizeValues(option.Values)
		if err != nil {
			return nil, err
		}
		result = append(result, Option{Name: name, Values: values, SortOrder: index})
	}
	return result, nil
}

func normalizeValues(values []string) ([]string, error) {
	trimmed := collection.Map(values, strings.TrimSpace)
	if collection.Contains(trimmed, func(value string) bool { return value == "" || len(value) > 80 }) {
		return nil, errors.New("option values must be valid")
	}
	seen := make(map[string]struct{}, len(trimmed))
	unique := collection.Filter(trimmed, func(value string) bool {
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		return true
	})
	if len(unique) < 1 || len(unique) > 30 {
		return nil, errors.New("each option must contain between 1 and 30 values")
	}
	return unique, nil
}
