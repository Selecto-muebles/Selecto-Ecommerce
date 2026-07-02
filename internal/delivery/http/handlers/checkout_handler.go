package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/delivery/http/middleware"
	"Selecto-Ecommerce/internal/domain"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/money"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type CheckoutRequest struct {
	OrderID utils.PublicID `json:"order_id"`
}

func CheckoutHandler(db *database.DB, cfg *config.Config, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input CheckoutRequest

		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}

		orderID := input.OrderID.Int()
		if orderID <= 0 {
			apperrors.BadRequest(c, "invalid order_id")
			return
		}

		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)

		var status string
		var total money.Cents
		var activePreferenceID sql.NullString
		var activeCheckoutURL sql.NullString
		var activeEnvironment sql.NullString
		err := db.Pool.QueryRow(
			c,
			`SELECT o.status, ROUND(o.total * 100)::BIGINT, o.active_payment_preference_id, o.active_checkout_url, o.active_payment_environment
			 FROM orders o
			 JOIN users u ON u.id = o.user_id
			 WHERE o.id=$1 AND u.email=$2
			   AND (
			     COALESCE(o.expires_at, o.created_at + make_interval(secs => $3)) > NOW()
			     OR o.active_payment_preference_id IS NOT NULL
			   )`,
			orderID,
			email,
			int(cfg.OrderPendingTTL.Seconds()),
		).Scan(&status, &total, &activePreferenceID, &activeCheckoutURL, &activeEnvironment)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				apperrors.JSON(c, http.StatusNotFound, apperrors.CodeNotFound, "order not found or expired", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		if domain.OrderStatus(status) != domain.OrderStatusPending {
			logger.Info(logging.EventCheckoutRejected, "order_id", orderID, "public_id", utils.EncodeID(orderID), "status", status)
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "order is not pending", gin.H{"status": status})
			return
		}

		if activePreferenceID.Valid && activeCheckoutURL.Valid && activePreferenceID.String != "" && activeCheckoutURL.String != "" {
			result := gin.H{
				"preference_id": activePreferenceID.String,
				"checkout_url":  activeCheckoutURL.String,
				"order_id":      utils.EncodeID(orderID),
			}
			if activeEnvironment.Valid && activeEnvironment.String != "" {
				result["environment"] = activeEnvironment.String
			}
			logger.Info(logging.EventCheckoutPreferenceReused, "order_id", orderID, "public_id", utils.EncodeID(orderID), "preference_id", activePreferenceID.String)
			c.JSON(http.StatusOK, result)
			return
		}

		if cfg.PaymentsServiceURL == "" {
			apperrors.Internal(c)
			return
		}

		logger.Info(logging.EventCheckoutStarted, "order_id", orderID, "public_id", utils.EncodeID(orderID), "amount_cents", int64(total))

		payload, _ := json.Marshal(gin.H{
			"order_id": orderID,
			"amount":   total.Float64(),
		})

		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, cfg.PaymentsServiceURL+"/create-preference", bytes.NewBuffer(payload))
		if err != nil {
			apperrors.Internal(c)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", middleware.RequestID(c))
		req.Header.Set("X-Correlation-ID", middleware.CorrelationID(c))

		resp, err := client.Do(req)
		if err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service unreachable", nil)
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "invalid payments service response", nil)
			return
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service rejected checkout", gin.H{"payments_response": result})
			return
		}

		preferenceID, _ := result["preference_id"].(string)
		checkoutURL, _ := result["checkout_url"].(string)
		environment, _ := result["environment"].(string)
		if preferenceID == "" || checkoutURL == "" {
			apperrors.JSON(c, http.StatusBadGateway, apperrors.CodeBadGateway, "payments service response missing preference", nil)
			return
		}

		commandTag, err := db.Pool.Exec(
			c,
			`UPDATE orders
			 SET active_payment_preference_id=$1,
			     active_checkout_url=$2,
			     active_payment_environment=$3
			 WHERE id=$4 AND status=$5 AND active_payment_preference_id IS NULL`,
			preferenceID,
			checkoutURL,
			environment,
			orderID,
			domain.OrderStatusPending,
		)
		if err != nil {
			apperrors.Internal(c)
			return
		}
		if commandTag.RowsAffected() == 0 {
			var existingPreferenceID, existingCheckoutURL, existingEnvironment sql.NullString
			if err := db.Pool.QueryRow(
				c,
				`SELECT active_payment_preference_id, active_checkout_url, active_payment_environment
				 FROM orders
				 WHERE id=$1 AND status=$2`,
				orderID,
				domain.OrderStatusPending,
			).Scan(&existingPreferenceID, &existingCheckoutURL, &existingEnvironment); err == nil && existingPreferenceID.Valid && existingCheckoutURL.Valid {
				result := gin.H{
					"preference_id": existingPreferenceID.String,
					"checkout_url":  existingCheckoutURL.String,
					"order_id":      utils.EncodeID(orderID),
				}
				if existingEnvironment.Valid {
					result["environment"] = existingEnvironment.String
				}
				logger.Info(logging.EventCheckoutPreferenceReused, "order_id", orderID, "public_id", utils.EncodeID(orderID), "preference_id", existingPreferenceID.String)
				c.JSON(http.StatusOK, result)
				return
			}
			apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "order is not pending", nil)
			return
		}

		if _, err := db.Pool.Exec(
			c,
			"INSERT INTO audit_logs (actor_email, action, entity_type, entity_id, metadata) VALUES ($1, $2, $3, $4, $5)",
			email,
			"checkout_preference_created",
			"order",
			orderID,
			fmt.Sprintf(`{"preference_id":%q}`, preferenceID),
		); err != nil {
			logger.Error(logging.EventCheckoutAuditFailed, "order_id", orderID, "public_id", utils.EncodeID(orderID), "preference_id", preferenceID, "error", err)
		}

		result["order_id"] = utils.EncodeID(orderID)

		logger.Info(logging.EventCheckoutPreferenceCreated, "order_id", orderID, "public_id", utils.EncodeID(orderID), "preference_id", preferenceID, "environment", environment)
		c.JSON(http.StatusOK, result)
	}
}
