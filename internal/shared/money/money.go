package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Cents int64

func FromFloat(value float64) (Cents, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, errors.New("invalid money amount")
	}
	return Cents(math.Round(value * 100)), nil
}

func FromDecimalString(value string) (Cents, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty money amount")
	}
	if strings.HasPrefix(value, "-") {
		return 0, errors.New("invalid money amount")
	}

	parts := strings.SplitN(value, ".", 3)
	if len(parts) > 2 {
		return 0, errors.New("invalid money amount")
	}

	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}

	var cents int64
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) > 2 {
			fraction = fraction[:2]
		}
		for len(fraction) < 2 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, err
		}
	}

	return Cents(whole*100 + cents), nil
}

func (c Cents) DecimalString() string {
	if c < 0 {
		return fmt.Sprintf("-%d.%02d", -c/100, -c%100)
	}
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}

func (c Cents) Float64() float64 {
	return float64(c) / 100
}
