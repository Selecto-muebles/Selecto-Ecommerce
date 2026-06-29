package handlers

import (
	"errors"
	"net/http"
	"strings"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/apperrors"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type RegisterInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func RegisterHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = cfg
		var input RegisterInput

		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))
		if input.Email == "" || !strings.Contains(input.Email, "@") || len(input.Password) < 8 {
			apperrors.BadRequest(c, "email and password must be valid")
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		// 👇 agregamos role por defecto
		_, err = db.Pool.Exec(
			c,
			"INSERT INTO users (email, password, role) VALUES ($1, $2, $3)",
			input.Email,
			string(hashedPassword),
			"user",
		)

		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				apperrors.JSON(c, http.StatusConflict, apperrors.CodeConflict, "user already exists", nil)
				return
			}
			apperrors.Internal(c)
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "user created"})
	}
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func LoginHandler(db *database.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input LoginInput

		if err := c.ShouldBindJSON(&input); err != nil {
			apperrors.BadRequest(c, "invalid input")
			return
		}
		input.Email = strings.ToLower(strings.TrimSpace(input.Email))

		var storedPassword string
		var role string

		err := db.Pool.QueryRow(
			c,
			"SELECT password, role FROM users WHERE email=$1",
			input.Email,
		).Scan(&storedPassword, &role)

		if err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(input.Password))
		if err != nil {
			apperrors.JSON(c, http.StatusUnauthorized, apperrors.CodeUnauthorized, "invalid credentials", nil)
			return
		}

		token, err := utils.GenerateToken(input.Email, role, cfg.JWTSecret, cfg.JWTTTL)
		if err != nil {
			apperrors.Internal(c)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token": token,
			"role":  role, // 👈 útil para frontend/admin
		})
	}
}
