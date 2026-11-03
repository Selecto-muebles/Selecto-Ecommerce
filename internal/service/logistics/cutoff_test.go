package logistics

import (
	"testing"
	"time"
)

func TestWeekendAndCutoff(t *testing.T) {
	location := buenosAires
	for _, test := range []struct {
		name string
		now  time.Time
		want string
	}{
		{"monday before", time.Date(2026, 11, 2, 12, 59, 0, 0, location), "2026-10-31T16:00:00Z"},
		{"monday at", time.Date(2026, 11, 2, 13, 0, 0, 0, location), "2026-11-02T16:00:00Z"},
		{"sunday", time.Date(2026, 11, 1, 18, 0, 0, 0, location), "2026-10-31T16:00:00Z"},
		{"saturday after", time.Date(2026, 10, 31, 13, 1, 0, 0, location), "2026-10-31T16:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := LatestDueCutoff(test.now).Format(time.RFC3339); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
	if got := NextCutoff(time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)); got.Format(time.RFC3339) != "2026-11-02T16:00:00Z" {
		t.Fatalf("Saturday must advance to Monday, got %s", got)
	}
}
