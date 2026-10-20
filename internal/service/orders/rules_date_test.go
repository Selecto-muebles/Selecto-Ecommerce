package orders

import (
	"testing"
	"time"
)

func TestParseRequestedDeliveryDateUsesArgentinaCalendar(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		date      string
		wantValid bool
	}{
		{
			name:      "tomorrow in Argentina before UTC midnight rollover",
			now:       time.Date(2026, 10, 20, 0, 14, 0, 0, time.UTC),
			date:      "2026-10-20",
			wantValid: true,
		},
		{
			name: "today in Argentina before UTC midnight rollover",
			now:  time.Date(2026, 10, 20, 0, 14, 0, 0, time.UTC),
			date: "2026-10-19",
		},
		{
			name: "today in Argentina after local midnight",
			now:  time.Date(2026, 10, 20, 3, 14, 0, 0, time.UTC),
			date: "2026-10-20",
		},
		{
			name:      "tomorrow in Argentina after local midnight",
			now:       time.Date(2026, 10, 20, 3, 14, 0, 0, time.UTC),
			date:      "2026-10-21",
			wantValid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseRequestedDeliveryDate(test.date, test.now)
			if (err == nil) != test.wantValid {
				t.Fatalf("ParseRequestedDeliveryDate(%q, %v) = %v, %v; want valid=%t", test.date, test.now, got, err, test.wantValid)
			}
			if test.wantValid && got.Format("2006-01-02") != test.date {
				t.Fatalf("got date %s, want %s", got.Format("2006-01-02"), test.date)
			}
		})
	}
}
