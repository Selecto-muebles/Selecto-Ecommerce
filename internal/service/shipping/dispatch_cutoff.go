package shipping

import (
	"fmt"
	"time"
)

const DefaultDispatchCutoffHour = 13
const DispatchTimezone = "America/Argentina/Buenos_Aires"

// DispatchCutoff returns the first Monday-Saturday cutoff strictly after payment.
// Sundays and Saturday afternoon roll forward to Monday's cutoff.
func DispatchCutoff(paidAt time.Time, hour int) (time.Time, error) {
	if hour < 0 || hour > 23 {
		return time.Time{}, fmt.Errorf("invalid dispatch cutoff hour %d", hour)
	}
	zone, err := time.LoadLocation(DispatchTimezone)
	if err != nil {
		return time.Time{}, err
	}
	local := paidAt.In(zone)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, zone)
	if !local.Before(cutoff) || local.Weekday() == time.Sunday {
		cutoff = cutoff.AddDate(0, 0, 1)
	}
	for cutoff.Weekday() == time.Sunday {
		cutoff = cutoff.AddDate(0, 0, 1)
	}
	return cutoff.UTC(), nil
}
