// Package scheduler triggers due test cycles on their cron schedules and exposes
// the cron parsing shared with the schedule API.
package scheduler

import (
	"fmt"
	"strings"
	"time"

	// Embed the IANA timezone database so per-cycle timezones resolve even on
	// hosts without system zoneinfo (notably Windows dev machines).
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

// Next returns the next fire time of a standard 5-field cron spec (or a
// @descriptor like @hourly / @every 1h) after `from`, interpreted in UTC.
func Next(spec string, from time.Time) (time.Time, error) {
	return NextIn(spec, "", from)
}

// NextIn is Next with an explicit IANA timezone (e.g. "America/New_York") in
// which the spec's wall-clock fields are interpreted. An empty tz means UTC.
// This is what makes "0 9 * * *" mean 9am where the user lives rather than 9am
// UTC; the returned time is still absolute.
func NextIn(spec, tz string, from time.Time) (time.Time, error) {
	sched, err := parse(spec, tz)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

// Valid reports whether a cron spec parses (in UTC).
func Valid(spec string) bool {
	_, err := parse(spec, "")
	return err == nil
}

// ValidTimezone reports whether tz is a loadable IANA zone. Empty is valid and
// means UTC.
func ValidTimezone(tz string) bool {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return true
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// parse builds a cron schedule bound to tz. robfig/cron reads a leading
// "CRON_TZ=<zone>" from the spec to bind the schedule's location, so we prepend
// one rather than reimplementing zone handling.
func parse(spec, tz string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("cron spec is empty")
	}
	if tz = strings.TrimSpace(tz); tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("unknown timezone %q: %w", tz, err)
		}
		if !strings.HasPrefix(spec, "CRON_TZ=") && !strings.HasPrefix(spec, "TZ=") {
			spec = "CRON_TZ=" + tz + " " + spec
		}
	}
	return cron.ParseStandard(spec)
}
