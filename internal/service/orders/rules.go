package orders

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"Selecto-Ecommerce/internal/shared/collection"
)

const (
	MaxItemsPerOrder         = 50
	MaxQuantityPerItem       = 100
	MaxTotalQuantityPerOrder = 100
)

var (
	ErrEmptyOrder         = errors.New("order must contain at least one item")
	ErrTooManyItems       = errors.New("too many items in order")
	ErrInvalidItem        = errors.New("product_id and quantity must be positive")
	ErrItemQuantityLimit  = errors.New("quantity exceeds per-item limit")
	ErrOrderQuantityLimit = errors.New("order quantity limit exceeded")
)

type Item struct {
	ProductID       int
	Quantity        int
	SelectedOptions map[string]string
}

type GroupedItem struct {
	ProductID       int
	Quantity        int
	SelectedOptions map[string]string
}

type PreparedItems struct {
	Grouped           []GroupedItem
	QuantityByProduct map[int]int
	ProductIDs        []int
}

func PrepareItems(items []Item) (PreparedItems, error) {
	if len(items) == 0 {
		return PreparedItems{}, ErrEmptyOrder
	}
	if len(items) > MaxItemsPerOrder {
		return PreparedItems{}, ErrTooManyItems
	}
	if collection.Contains(items, func(item Item) bool { return item.ProductID <= 0 || item.Quantity <= 0 }) {
		return PreparedItems{}, ErrInvalidItem
	}
	if collection.Contains(items, func(item Item) bool { return item.Quantity > MaxQuantityPerItem }) {
		return PreparedItems{}, ErrItemQuantityLimit
	}
	total := collection.Reduce(items, 0, func(sum int, item Item) int { return sum + item.Quantity })
	if total > MaxTotalQuantityPerOrder {
		return PreparedItems{}, ErrOrderQuantityLimit
	}

	grouped, quantities, err := groupItems(items)
	if err != nil {
		return PreparedItems{}, err
	}
	return PreparedItems{
		Grouped:           grouped,
		QuantityByProduct: quantities,
		ProductIDs:        collection.SortedIntKeys(quantities),
	}, nil
}

func ParseRequestedDeliveryDate(value string, now time.Time) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	requested, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, errors.New("requested_delivery_date must use YYYY-MM-DD")
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if !requested.After(today) {
		return nil, errors.New("requested_delivery_date must be after today")
	}
	return &requested, nil
}

func groupItems(items []Item) ([]GroupedItem, map[int]int, error) {
	grouped := make([]GroupedItem, 0, len(items))
	groupIndex := make(map[string]int, len(items))
	quantities := collection.GroupSumByInt(items, func(item Item) int { return item.ProductID }, func(item Item) int { return item.Quantity })

	for _, item := range items {
		options := item.SelectedOptions
		if options == nil {
			options = map[string]string{}
		}
		raw, err := json.Marshal(options)
		if err != nil {
			return nil, nil, err
		}
		key := fmt.Sprintf("%d:%s", item.ProductID, raw)
		if index, exists := groupIndex[key]; exists {
			grouped[index].Quantity += item.Quantity
			continue
		}
		groupIndex[key] = len(grouped)
		grouped = append(grouped, GroupedItem{ProductID: item.ProductID, Quantity: item.Quantity, SelectedOptions: options})
	}
	return grouped, quantities, nil
}
