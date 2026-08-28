package email

import "context"

// DispatchNotifier is implemented by any backend that can asynchronously
// notify a worker to process a single outbox entry identified by its ID.
type DispatchNotifier interface {
	Notify(context.Context, int64)
}

// NotifyAfterCommit fires each notifier for the given outbox entry after a
// database transaction has been committed. It is a no-op when outboxID <= 0
// (i.e. the email was already deduplicated and not inserted).
func NotifyAfterCommit(ctx context.Context, outboxID int64, notifiers ...DispatchNotifier) {
	if outboxID <= 0 {
		return
	}
	dispatchContext := context.WithoutCancel(ctx)
	for _, notifier := range notifiers {
		if notifier != nil {
			notifier.Notify(dispatchContext, outboxID)
		}
	}
}
