// Package scheduler triggers due test cycles on their cron schedules and exposes
// the cron parsing shared with the schedule API.
package scheduler

import (
	"time"

	"github.com/robfig/cron/v3"
)

// Next returns the next fire time of a standard 5-field cron spec (or a
// @descriptor like @hourly / @every 1h) after `from`.
func Next(spec string, from time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

// Valid reports whether a cron spec parses.
func Valid(spec string) bool {
	_, err := cron.ParseStandard(spec)
	return err == nil
}
