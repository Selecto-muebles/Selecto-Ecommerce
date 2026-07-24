package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type orderStockItem struct {
	productID int
	quantity  int
}

func ReserveOrderStock(ctx context.Context, tx pgx.Tx, orderID int) error {
	return applyOrderStockItems(ctx, tx, orderID, func(item orderStockItem) error {
		commandTag, err := tx.Exec(ctx, "UPDATE products SET stock = stock - $1 WHERE id=$2 AND stock >= $1", item.quantity, item.productID)
		if err != nil {
			return err
		}
		if commandTag.RowsAffected() == 0 {
			return fmt.Errorf("insufficient stock for product_id=%d", item.productID)
		}
		return nil
	})
}

func RestoreOrderStock(ctx context.Context, tx pgx.Tx, orderID int) error {
	return applyOrderStockItems(ctx, tx, orderID, func(item orderStockItem) error {
		_, err := tx.Exec(ctx, "UPDATE products SET stock = stock + $1 WHERE id=$2", item.quantity, item.productID)
		return err
	})
}

func applyOrderStockItems(ctx context.Context, tx pgx.Tx, orderID int, apply func(orderStockItem) error) error {
	rows, err := tx.Query(ctx, "SELECT product_id, quantity FROM order_items WHERE order_id=$1", orderID)
	if err != nil {
		return err
	}
	defer rows.Close()

	items := make([]orderStockItem, 0)
	for rows.Next() {
		var item orderStockItem
		if err := rows.Scan(&item.productID, &item.quantity); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range items {
		if err := apply(item); err != nil {
			return err
		}
	}
	return nil
}
