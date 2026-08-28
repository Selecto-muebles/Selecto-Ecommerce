package email

import (
	"context"
	"testing"
)

type recordingNotifier struct {
	ids []int64
}

func (notifier *recordingNotifier) Notify(_ context.Context, id int64) {
	notifier.ids = append(notifier.ids, id)
}

func TestNotifyAfterCommitDispatchesPersistedOutboxID(t *testing.T) {
	notifier := &recordingNotifier{}
	NotifyAfterCommit(context.Background(), 42, notifier)
	if len(notifier.ids) != 1 || notifier.ids[0] != 42 {
		t.Fatalf("notified IDs = %v, want [42]", notifier.ids)
	}
}

func TestNotifyAfterCommitIgnoresMissingOutboxID(t *testing.T) {
	notifier := &recordingNotifier{}
	NotifyAfterCommit(context.Background(), 0, notifier)
	if len(notifier.ids) != 0 {
		t.Fatalf("notified IDs = %v, want none", notifier.ids)
	}
}

func TestNotifyAfterCommitDispatchesToEveryConfiguredNotifier(t *testing.T) {
	first := &recordingNotifier{}
	second := &recordingNotifier{}
	NotifyAfterCommit(context.Background(), 42, first, nil, second)
	if len(first.ids) != 1 || first.ids[0] != 42 {
		t.Fatalf("first notifier IDs = %v, want [42]", first.ids)
	}
	if len(second.ids) != 1 || second.ids[0] != 42 {
		t.Fatalf("second notifier IDs = %v, want [42]", second.ids)
	}
}
