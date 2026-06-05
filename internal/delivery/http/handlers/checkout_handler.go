package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type CheckoutRequest struct {
	OrderID utils.PublicID `json:"order_id"`
}

func CheckoutHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input CheckoutRequest

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		orderID := input.OrderID.Int()
		if orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order_id"})
			return
		}

		emailValue, _ := c.Get("email")
		email := fmt.Sprint(emailValue)

		var status string
		var total float64
		err := db.Pool.QueryRow(
			c,
			`SELECT o.status, o.total
			 FROM orders o
			 JOIN users u ON u.id = o.user_id
			 WHERE o.id=$1 AND u.email=$2`,
			orderID,
			email,
		).Scan(&status, &total)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if status != "pending" {
			log.Printf("checkout rejected: order_id=%d public_id=%s status=%s", orderID, utils.EncodeID(orderID), status)
			c.JSON(http.StatusConflict, gin.H{"error": "order is not pending", "status": status})
			return
		}

		paymentsURL := os.Getenv("PAYMENTS_SERVICE_URL")
		if paymentsURL == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "PAYMENTS_SERVICE_URL is not configured"})
			return
		}

		log.Printf("checkout started: order_id=%d public_id=%s amount=%.2f", orderID, utils.EncodeID(orderID), total)

		payload, _ := json.Marshal(gin.H{
			"order_id": orderID,
			"amount":   total,
		})

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(paymentsURL+"/create-preference", "application/json", bytes.NewBuffer(payload))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "payments service unreachable"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid payments service response"})
			return
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":   "payments service rejected checkout",
				"details": result,
			})
			return
		}

		result["order_id"] = utils.EncodeID(orderID)

		c.JSON(http.StatusOK, result)
	}
}
