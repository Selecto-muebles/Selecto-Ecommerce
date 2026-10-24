package shipping

import (
	"testing"
	"time"
)

func TestDispatchCutoff(t *testing.T) {
	zone, err := time.LoadLocation(DispatchTimezone)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ paid, want string }{
		{"2026-10-19 12:59", "2026-10-19 13:00"}, // Monday before cutoff
		{"2026-10-19 13:00", "2026-10-20 13:00"}, // exactly at cutoff
		{"2026-10-24 12:59", "2026-10-24 13:00"}, // Saturday
		{"2026-10-24 13:01", "2026-10-26 13:00"}, // Saturday afternoon
		{"2026-10-25 08:00", "2026-10-26 13:00"}, // Sunday
	}
	for _, tt := range tests {
		paid, err := time.ParseInLocation("2006-01-02 15:04", tt.paid, zone)
		if err != nil {
			t.Fatal(err)
		}
		want, err := time.ParseInLocation("2006-01-02 15:04", tt.want, zone)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DispatchCutoff(paid, DefaultDispatchCutoffHour)
		if err != nil || !got.Equal(want) {
			t.Errorf("paid=%s got=%s want=%s err=%v", tt.paid, got.In(zone), want, err)
		}
	}
}
