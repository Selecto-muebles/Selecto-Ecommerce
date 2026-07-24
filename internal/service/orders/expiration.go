package orders

import (
	"context"
	"time"
)

type ExpirationRepository interface {
	ReleaseExpired(context.Context, time.Duration, int, bool) (int64, error)
}

type Expirer struct {
	repository ExpirationRepository
}

func NewExpirer(repository ExpirationRepository) *Expirer {
	return &Expirer{repository: repository}
}

func (service *Expirer) Release(ctx context.Context, olderThan time.Duration, batchSize int, writeAudit bool) (int64, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	return service.repository.ReleaseExpired(ctx, olderThan, batchSize, writeAudit)
}
