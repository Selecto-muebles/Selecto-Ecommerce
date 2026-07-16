package handlers

import (
	"testing"

	"Selecto-Ecommerce/internal/shared/utils"
)

func TestGroupOrderItemsKeepsDifferentVariantsSeparate(t *testing.T) {
	items := []OrderItemInput{
		{ProductID: utils.PublicID(42), Quantity: 1, SelectedOptions: map[string]string{"Color": "Negro", "Modelo": "Bajas"}},
		{ProductID: utils.PublicID(42), Quantity: 2, SelectedOptions: map[string]string{"Modelo": "Altas", "Color": "Madera natural"}},
		{ProductID: utils.PublicID(42), Quantity: 3, SelectedOptions: map[string]string{"Modelo": "Bajas", "Color": "Negro"}},
	}

	grouped, quantityByProduct, err := groupOrderItems(items)
	if err != nil {
		t.Fatalf("groupOrderItems() error = %v", err)
	}
	if len(grouped) != 2 {
		t.Fatalf("len(grouped) = %d, want 2", len(grouped))
	}
	if grouped[0].Quantity != 4 {
		t.Errorf("first variant quantity = %d, want 4", grouped[0].Quantity)
	}
	if grouped[1].Quantity != 2 {
		t.Errorf("second variant quantity = %d, want 2", grouped[1].Quantity)
	}
	if quantityByProduct[42] != 6 {
		t.Errorf("product quantity = %d, want 6", quantityByProduct[42])
	}
}

func TestGroupOrderItemsNormalizesMissingOptions(t *testing.T) {
	items := []OrderItemInput{
		{ProductID: utils.PublicID(7), Quantity: 1},
		{ProductID: utils.PublicID(7), Quantity: 2, SelectedOptions: map[string]string{}},
	}

	grouped, quantityByProduct, err := groupOrderItems(items)
	if err != nil {
		t.Fatalf("groupOrderItems() error = %v", err)
	}
	if len(grouped) != 1 || grouped[0].Quantity != 3 {
		t.Fatalf("grouped = %#v, want one item with quantity 3", grouped)
	}
	if quantityByProduct[7] != 3 {
		t.Errorf("product quantity = %d, want 3", quantityByProduct[7])
	}
}
