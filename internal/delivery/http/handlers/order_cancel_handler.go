package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func CancelOrderHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		orderID, err := utils.DecodeID(c.Param("id"))
		if err != nil || orderID <= 0 {
			apperrors.BadRequest(c, "invalid order id")
			return
		}
		email := strings.ToLower(strings.TrimSpace(fmt.Sprint(c.MustGet("email"))))
		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)
		var status string
		var activePreference string
		err = tx.QueryRow(c, `SELECT o.status, COALESCE(o.active_payment_preference_id, '') FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1 AND u.email=$2 FOR UPDATE OF o`, orderID, email).Scan(&status, &activePreference)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "order not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}
		if status == string(domain.OrderStatusCancelled) {
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			order, err := fetchOrder(c, db, orderID, email, false)
			if err != nil {
				apperrors.Internal(c)
				return
			}
			c.JSON(http.StatusOK, order)
			return
		}
		if status != string(domain.OrderStatusPending) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "only unpaid pending orders can be cancelled", gin.H{"status": status})
			return
		}
		if activePreference != "" {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "payment was already initiated; wait for its final status before requesting cancellation", gin.H{"payment_initiated": true})
			return
		}
		if err := postgresrepo.RestoreOrderStock(c, tx, orderID); err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "UPDATE orders SET status='cancelled', payment_status='cancelled', cancelled_at=NOW() WHERE id=$1", orderID); err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, 'order_cancelled_by_customer', 'order', $2, $3)", email, orderID, `{"previous_status":"pending"}`); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := enqueuePaymentStatusEmail(c, tx, cfg, orderID, 0, email, "cancelled"); err != nil {
			apperrors.Internal(c)
			return
		}
		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		logger.Info("customer_order_cancelled", "order_id", orderID)
		order, err := fetchOrder(c, db, orderID, email, false)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		c.JSON(http.StatusOK, order)
	}
}
