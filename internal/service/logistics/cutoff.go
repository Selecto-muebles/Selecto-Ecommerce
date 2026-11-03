package logistics

import "time"

const CutoffHour = 13

var buenosAires = func() *time.Location {
	location, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		panic(err)
	}
	return location
}()

func LatestDueCutoff(now time.Time) time.Time {
	local := now.In(buenosAires)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), CutoffHour, 0, 0, 0, buenosAires)
	if local.Before(cutoff) || local.Weekday() == time.Sunday {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	for cutoff.Weekday() == time.Sunday {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	return cutoff.UTC()
}

func NextCutoff(previous time.Time) time.Time {
	local := previous.In(buenosAires).AddDate(0, 0, 1)
	for local.Weekday() == time.Sunday {
		local = local.AddDate(0, 0, 1)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), CutoffHour, 0, 0, 0, buenosAires).UTC()
}
