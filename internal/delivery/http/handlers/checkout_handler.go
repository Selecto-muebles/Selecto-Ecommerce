package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type CheckoutRequest struct {
	OrderID int `json:"order_id"`
}

func CheckoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {

		var input CheckoutRequest

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		// 👉 URL del payments service
		paymentsURL := os.Getenv("PAYMENTS_SERVICE_URL")

		payload, _ := json.Marshal(input)

		resp, err := http.Post(
			paymentsURL+"/create-preference",
			"application/json",
			bytes.NewBuffer(payload),
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "payments service unreachable"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		c.JSON(http.StatusOK, result)
	}
}