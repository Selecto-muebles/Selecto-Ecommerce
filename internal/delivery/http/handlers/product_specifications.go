package handlers

import (
	"encoding/json"
	"errors"
	"strings"
)

const maxProductSpecifications = 24

type productSpecifications map[string]string

func normalizeSpecifications(input productSpecifications) (productSpecifications, error) {
	if len(input) > maxProductSpecifications {
		return nil, errors.New("too many specifications")
	}
	result := productSpecifications{}
	for key, value := range input {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || len(key) > 80 || len(value) > 240 {
			return nil, errors.New("specification names and values must be valid")
		}
		result[key] = value
	}
	return result, nil
}

func encodeSpecifications(input productSpecifications) ([]byte, error) {
	if input == nil {
		input = productSpecifications{}
	}
	return json.Marshal(input)
}

func decodeSpecifications(raw []byte) (productSpecifications, error) {
	result := productSpecifications{}
	if len(raw) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}
