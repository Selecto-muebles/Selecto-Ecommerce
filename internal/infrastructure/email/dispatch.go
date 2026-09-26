package email

import "context"

type DispatchNotifier interface {
	Notify(context.Context, int64)
}

type MarketingDispatchNotifier interface {
	NotifyMarketing(context.Context, int64)
}

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

func NotifyMarketingAfterCommit(ctx context.Context, syncID int64, notifiers ...MarketingDispatchNotifier) {
	if syncID <= 0 {
		return
	}
	dispatchContext := context.WithoutCancel(ctx)
	for _, notifier := range notifiers {
		if notifier != nil {
			notifier.NotifyMarketing(dispatchContext, syncID)
		}
	}
}
