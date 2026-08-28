package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	paymentservice "Selecto-Ecommerce/internal/service/payments"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/money"
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

func PaymentWebhookHandler(db *database.DB, cfg *config.Config, logger *slog.Logger, notifiers ...mailinfra.DispatchNotifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input PaymentWebhookInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid payload")
			return
		}

		orderID := input.OrderID.Int()
		if input.PaymentID <= 0 || orderID <= 0 {
			apperrors.BadRequest(c, "payment_id and order_id must be positive")
			return
		}

		newStatus, err := paymentservice.NormalizeStatus(input.Status)
		if err != nil {
			apperrors.BadRequest(c, "invalid status")
			return
		}

		logger.Info(logging.EventPaymentWebhookReceived, "payment_id", input.PaymentID, "order_id", orderID, "public_id", utils.EncodeID(orderID), "status", newStatus)

		tx, err := db.Pool.Begin(c)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		defer tx.Rollback(c)

		var currentStatus string
		var orderTotal money.Cents
		var customerEmail string
		var existingPaymentID sql.NullInt64
		var activePreferenceID sql.NullString
		err = tx.QueryRow(
			c,
			"SELECT o.status, ROUND(o.total * 100)::BIGINT, o.payment_id, o.active_payment_preference_id, u.email FROM orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1 FOR UPDATE OF o",
			orderID,
		).Scan(&currentStatus, &orderTotal, &existingPaymentID, &activePreferenceID, &customerEmail)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "order not found", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		var amountValue interface{}
		if input.Amount != nil {
			amountValue = *input.Amount
		}
		eventKey := fmt.Sprintf("%d:%d:%s", input.PaymentID, orderID, newStatus)
		commandTag, err := tx.Exec(
			c,
			`INSERT INTO payment_webhook_events (event_key, payment_id, order_id, status, amount_cents)
			 VALUES ($1, $2, $3, $4, CASE WHEN $5::NUMERIC IS NULL THEN NULL ELSE ROUND($5::NUMERIC * 100)::BIGINT END)
			 ON CONFLICT (event_key) DO NOTHING`,
			eventKey,
			input.PaymentID,
			orderID,
			newStatus,
			amountValue,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if commandTag.RowsAffected() == 0 {
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": currentStatus})
			return
		}

		if currentStatus == "paid" {
			logger.Info(logging.EventPaymentWebhookIgnoredPaid, "payment_id", input.PaymentID, "order_id", orderID)
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "paid"})
			return
		}

		if newStatus != "paid" {
			_, err = tx.Exec(
				c,
				`UPDATE orders
				 SET payment_status=$1,
				     payment_id=$2
				 WHERE id=$3`,
				newStatus,
				input.PaymentID,
				orderID,
			)
			if err != nil {
				apperrors.Internal(c)
				return
			}
			if _, err := tx.Exec(
				c,
				"UPDATE payment_webhook_events SET processed_at=NOW(), result=$1 WHERE event_key=$2",
				newStatus,
				eventKey,
			); err != nil {
				apperrors.Internal(c)
				return
			}
			outboxID, err := enqueuePaymentStatusEmail(c, tx, cfg, orderID, input.PaymentID, customerEmail, newStatus)
			if err != nil {
				apperrors.Internal(c)
				return
			}
			if _, err := tx.Exec(
				c,
				"INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)",
				"payments-service",
				"payment_webhook_recorded",
				"order",
				orderID,
				fmt.Sprintf(`{"payment_id":%d,"payment_status":%q,"order_status":%q}`, input.PaymentID, newStatus, currentStatus),
			); err != nil {
				apperrors.Internal(c)
				return
			}
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)

			logger.Info(logging.EventPaymentWebhookRecorded, "payment_id", input.PaymentID, "order_id", orderID, "public_id", utils.EncodeID(orderID), "payment_status", newStatus, "order_status", currentStatus)

			c.JSON(http.StatusOK, gin.H{"status": currentStatus, "payment_status": newStatus})
			return
		}

		recoveringPaidOrder := paymentservice.CanRecoverPaidOrder(domain.OrderStatus(currentStatus), activePreferenceID.Valid && activePreferenceID.String != "")
		if !domain.CanTransitionOrder(domain.OrderStatus(currentStatus), domain.OrderStatusPaid) && !recoveringPaidOrder {
			logger.Info(logging.EventPaymentWebhookIgnoredTransition, "payment_id", input.PaymentID, "order_id", orderID, "current_status", currentStatus, "requested_status", newStatus)
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": currentStatus})
			return
		}

		if !paymentservice.AmountMatches(input.Amount, orderTotal) {
			if input.Amount == nil {
				logger.Warn(logging.EventPaymentWebhookAmountRejected, "payment_id", input.PaymentID, "order_id", orderID, "expected_cents", int64(orderTotal), "reason", "missing_amount")
			} else {
				logger.Warn(logging.EventPaymentWebhookAmountRejected, "payment_id", input.PaymentID, "order_id", orderID, "expected_cents", int64(orderTotal), "received_amount", *input.Amount, "reason", "amount_mismatch")
			}
			if _, err := tx.Exec(c, "UPDATE payment_webhook_events SET processed_at=NOW(), result=$1 WHERE event_key=$2", "ignored_amount_mismatch", eventKey); err != nil {
				apperrors.Internal(c)
				return
			}
			if err := tx.Commit(c); err != nil {
				apperrors.Internal(c)
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "amount_mismatch"})
			return
		}

		if recoveringPaidOrder {
			if err := postgresrepo.ReserveOrderStock(c, tx, orderID); err != nil {
				logger.Error(logging.EventPaymentWebhookStockFailed, "payment_id", input.PaymentID, "order_id", orderID, "error", err)
				c.JSON(http.StatusConflict, gin.H{"status": currentStatus, "reason": "stock_unavailable"})
				return
			}
		}

		if existingPaymentID.Valid && existingPaymentID.Int64 != int64(input.PaymentID) && currentStatus == string(domain.OrderStatusPaid) {
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "order already has a different payment", nil)
			return
		}

		_, err = tx.Exec(
			c,
			`UPDATE orders
			 SET status=$1,
			     payment_status=$1,
			     payment_id=$2,
			     paid_at=CASE WHEN $1='paid' THEN NOW() ELSE paid_at END,
			     cancelled_at=CASE WHEN $1='paid' THEN NULL WHEN $1 IN ('failed', 'cancelled') THEN NOW() ELSE cancelled_at END
			 WHERE id=$3`,
			newStatus,
			input.PaymentID,
			orderID,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(c, "INSERT INTO shipments (order_id, status) VALUES ($1, $2) ON CONFLICT (order_id) DO NOTHING", orderID, domain.ShipmentStatusPreparing); err != nil {
			apperrors.Internal(c)
			return
		}

		if _, err := tx.Exec(
			c,
			"UPDATE payment_webhook_events SET processed_at=NOW(), result=$1 WHERE event_key=$2",
			newStatus,
			eventKey,
		); err != nil {
			apperrors.Internal(c)
			return
		}
		outboxID, err := enqueuePaymentStatusEmail(c, tx, cfg, orderID, input.PaymentID, customerEmail, newStatus)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if _, err := tx.Exec(
			c,
			"INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)",
			"payments-service",
			"payment_webhook_applied",
			"order",
			orderID,
			fmt.Sprintf(`{"payment_id":%d,"status":%q}`, input.PaymentID, newStatus),
		); err != nil {
			apperrors.Internal(c)
			return
		}

		if err := tx.Commit(c); err != nil {
			apperrors.Internal(c)
			return
		}
		mailinfra.NotifyAfterCommit(c.Request.Context(), outboxID, notifiers...)

		logger.Info(logging.EventPaymentWebhookApplied, "payment_id", input.PaymentID, "order_id", orderID, "public_id", utils.EncodeID(orderID), "status", newStatus)

		c.JSON(http.StatusOK, gin.H{"status": newStatus})
	}
}
