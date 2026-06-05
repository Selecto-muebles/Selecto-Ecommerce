package handlers

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type PaymentWebhookInput struct {
	PaymentID int            `json:"payment_id" binding:"required"`
	OrderID   utils.PublicID `json:"order_id" binding:"required"`
	Amount    *float64       `json:"amount"`
	Status    string         `json:"status"`
}

func PaymentWebhookHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input PaymentWebhookInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}

		orderID := input.OrderID.Int()
		if input.PaymentID <= 0 || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "payment_id and order_id must be positive"})
			return
		}

		newStatus := input.Status
		if newStatus == "" {
			newStatus = "paid"
		}
		if newStatus != "paid" && newStatus != "failed" && newStatus != "cancelled" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
			return
		}

		log.Printf("payment webhook received: payment_id=%d order_id=%d public_id=%s status=%s", input.PaymentID, orderID, utils.EncodeID(orderID), newStatus)

		tx, err := db.Pool.Begin(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start transaction"})
			return
		}
		defer tx.Rollback(c)

		var currentStatus string
		var orderTotal float64
		err = tx.QueryRow(
			c,
			"SELECT status, total FROM orders WHERE id=$1 FOR UPDATE",
			orderID,
		).Scan(&currentStatus, &orderTotal)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if currentStatus == "paid" {
			log.Printf("payment webhook ignored: order already paid payment_id=%d order_id=%d", input.PaymentID, orderID)
			if err := tx.Commit(c); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit webhook"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "paid"})
			return
		}

		if currentStatus == newStatus {
			log.Printf("payment webhook ignored: duplicate status payment_id=%d order_id=%d status=%s", input.PaymentID, orderID, currentStatus)
			if err := tx.Commit(c); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit webhook"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": currentStatus})
			return
		}

		if currentStatus != "pending" {
			log.Printf("payment webhook ignored: order already finalized payment_id=%d order_id=%d current_status=%s requested_status=%s", input.PaymentID, orderID, currentStatus, newStatus)
			if err := tx.Commit(c); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit webhook"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": currentStatus})
			return
		}

		if newStatus == "paid" && !amountMatches(input.Amount, orderTotal) {
			if input.Amount == nil {
				log.Printf("payment amount missing: payment_id=%d order_id=%d expected=%.2f", input.PaymentID, orderID, orderTotal)
			} else {
				log.Printf("payment amount mismatch: payment_id=%d order_id=%d expected=%.2f received=%.2f", input.PaymentID, orderID, orderTotal, *input.Amount)
			}
			if err := tx.Commit(c); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit webhook"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "amount_mismatch"})
			return
		}

		if newStatus == "failed" || newStatus == "cancelled" {
			if err := restoreOrderStock(c, tx, orderID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		_, err = tx.Exec(
			c,
			"UPDATE orders SET status=$1 WHERE id=$2",
			newStatus,
			orderID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if err := tx.Commit(c); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit webhook"})
			return
		}

		log.Printf("order updated from payment webhook: payment_id=%d order_id=%d public_id=%s status=%s", input.PaymentID, orderID, utils.EncodeID(orderID), newStatus)

		c.JSON(http.StatusOK, gin.H{"status": newStatus})
	}
}

func amountMatches(received *float64, expected float64) bool {
	if received == nil {
		return false
	}

	return math.Round(*received*100) == math.Round(expected*100)
}

func restoreOrderStock(ctx context.Context, tx pgx.Tx, orderID int) error {
	rows, err := tx.Query(
		ctx,
		"SELECT product_id, quantity FROM order_items WHERE order_id=$1",
		orderID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		productID int
		quantity  int
	}
	items := make([]item, 0)
	for rows.Next() {
		var current item
		if err := rows.Scan(&current.productID, &current.quantity); err != nil {
			return err
		}
		items = append(items, current)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, current := range items {
		if _, err := tx.Exec(
			ctx,
			"UPDATE products SET stock = stock + $1 WHERE id=$2",
			current.quantity,
			current.productID,
		); err != nil {
			return err
		}
	}

	return nil
}
