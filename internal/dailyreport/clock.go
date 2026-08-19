// Package dailyreport implements scheduled and on-demand AI daily reports.
package dailyreport

import "time"

// Clock makes scheduling deterministic in tests.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Timer is the subset of time.Timer used by the scheduler.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NewTimer(d time.Duration) Timer {
	return realTimer{Timer: time.NewTimer(d)}
}

type realTimer struct{ *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.Timer.C }

// RealClock returns the production clock.
func RealClock() Clock { return realClock{} }

// ScheduledBoundary returns the latest local scheduled boundary not after now.
// It constructs the wall-clock time in the requested location so adjacent
// boundaries naturally span 23 or 25 hours across daylight-saving changes.
func ScheduledBoundary(now time.Time, schedule string, location *time.Location) (time.Time, error) {
	hour, minute, err := parseScheduleTime(schedule)
	if err != nil {
		return time.Time{}, err
	}
	if location == nil {
		location = time.Local
	}
	localNow := now.In(location)
	boundary := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if boundary.After(localNow) {
		previous := localNow.AddDate(0, 0, -1)
		boundary = time.Date(previous.Year(), previous.Month(), previous.Day(), hour, minute, 0, 0, location)
	}
	return boundary, nil
}

// PreviousBoundary returns the scheduled wall-clock boundary on the previous
// local calendar day. Do not use Add(-24*time.Hour), which is wrong at DST.
func PreviousBoundary(boundary time.Time, schedule string, location *time.Location) (time.Time, error) {
	hour, minute, err := parseScheduleTime(schedule)
	if err != nil {
		return time.Time{}, err
	}
	if location == nil {
		location = boundary.Location()
	}
	previous := boundary.In(location).AddDate(0, 0, -1)
	return time.Date(previous.Year(), previous.Month(), previous.Day(), hour, minute, 0, 0, location), nil
}

// NextBoundary returns the next local calendar-day boundary.
func NextBoundary(boundary time.Time, schedule string, location *time.Location) (time.Time, error) {
	hour, minute, err := parseScheduleTime(schedule)
	if err != nil {
		return time.Time{}, err
	}
	if location == nil {
		location = boundary.Location()
	}
	next := boundary.In(location).AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), hour, minute, 0, 0, location), nil
}
