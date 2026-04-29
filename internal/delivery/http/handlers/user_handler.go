package handlers

import (
	"net/http"
	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type RegisterInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func RegisterHandler(db *database.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input RegisterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}

		_, err = db.Pool.Exec(
			c,
			"INSERT INTO users (email, password) VALUES ($1, $2)",
			input.Email,
			string(hashedPassword),
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "user created"})
	}
}