package handlers

import (
	"testing"

	orderservice "Selecto-Ecommerce/internal/service/orders"
)

func TestGroupOrderItemsKeepsDifferentVariantsSeparate(t *testing.T) {
	items := []orderservice.Item{
		{ProductID: 42, Quantity: 1, SelectedOptions: map[string]string{"Color": "Negro", "Modelo": "Bajas"}},
		{ProductID: 42, Quantity: 2, SelectedOptions: map[string]string{"Modelo": "Altas", "Color": "Madera natural"}},
		{ProductID: 42, Quantity: 3, SelectedOptions: map[string]string{"Modelo": "Bajas", "Color": "Negro"}},
	}

	prepared, err := orderservice.PrepareItems(items)
	if err != nil {
		t.Fatalf("groupOrderItems() error = %v", err)
	}
	if len(prepared.Grouped) != 2 {
		t.Fatalf("len(grouped) = %d, want 2", len(prepared.Grouped))
	}
	if prepared.Grouped[0].Quantity != 4 {
		t.Errorf("first variant quantity = %d, want 4", prepared.Grouped[0].Quantity)
	}
	if prepared.Grouped[1].Quantity != 2 {
		t.Errorf("second variant quantity = %d, want 2", prepared.Grouped[1].Quantity)
	}
	if prepared.QuantityByProduct[42] != 6 {
		t.Errorf("product quantity = %d, want 6", prepared.QuantityByProduct[42])
	}
}

func TestGroupOrderItemsNormalizesMissingOptions(t *testing.T) {
	items := []orderservice.Item{
		{ProductID: 7, Quantity: 1},
		{ProductID: 7, Quantity: 2, SelectedOptions: map[string]string{}},
	}

	prepared, err := orderservice.PrepareItems(items)
	if err != nil {
		t.Fatalf("groupOrderItems() error = %v", err)
	}
	if len(prepared.Grouped) != 1 || prepared.Grouped[0].Quantity != 3 {
		t.Fatalf("grouped = %#v, want one item with quantity 3", prepared.Grouped)
	}
	if prepared.QuantityByProduct[7] != 3 {
		t.Errorf("product quantity = %d, want 3", prepared.QuantityByProduct[7])
	}
}
