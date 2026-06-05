package utils

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

const defaultIDSecret uint64 = 0x5E1EC70

type PublicID int

func EncodeID(id int) string {
	if id <= 0 {
		return ""
	}

	encoded := uint64(id) ^ idSecret()
	return strings.ToUpper(strconv.FormatUint(encoded, 36))
}

func DecodeID(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty id")
	}

	if isDecimal(value) {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			return 0, errors.New("invalid id")
		}
		return id, nil
	}

	encoded, err := strconv.ParseUint(strings.ToUpper(value), 36, 64)
	if err != nil {
		return 0, errors.New("invalid id")
	}

	id := int(encoded ^ idSecret())
	if id <= 0 {
		return 0, errors.New("invalid id")
	}

	return id, nil
}

func (id *PublicID) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		if number <= 0 {
			return errors.New("invalid id")
		}
		*id = PublicID(number)
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("invalid id")
	}

	decoded, err := DecodeID(value)
	if err != nil {
		return err
	}

	*id = PublicID(decoded)
	return nil
}

func (id PublicID) Int() int {
	return int(id)
}

func idSecret() uint64 {
	raw := strings.TrimSpace(os.Getenv("PUBLIC_ID_SECRET"))
	if raw == "" {
		return defaultIDSecret
	}

	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || parsed == 0 {
		return defaultIDSecret
	}

	return parsed
}

func isDecimal(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
