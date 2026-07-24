package handlers

import (
	"context"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	orderservice "Selecto-Ecommerce/internal/service/orders"
)

func ReleaseExpiredPendingOrders(ctx context.Context, db *database.DB, olderThan time.Duration) (int64, error) {
	return expirationService(db).Release(ctx, olderThan, 100, false)
}

func ReleaseExpiredPendingOrdersWithAudit(ctx context.Context, db *database.DB, olderThan time.Duration, batchSize int) (int64, error) {
	return expirationService(db).Release(ctx, olderThan, batchSize, true)
}

func expirationService(db *database.DB) *orderservice.Expirer {
	return orderservice.NewExpirer(postgresrepo.NewOrderRepository(db, nil))
}
