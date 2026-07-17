package scheduler

import (
	"testing"
	"time"
)

// TestNextInTimezone is the crux of per-cycle scheduling: "0 9 * * *" must mean
// 9am *where the user is*, not 9am UTC.
func TestNextInTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load zone (is time/tzdata embedded?): %v", err)
	}
	// 2026-07-17 06:00 UTC = 02:00 EDT, so the next 9am EDT is 13:00 UTC.
	from := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)

	got, err := NextIn("0 9 * * *", "America/New_York", from)
	if err != nil {
		t.Fatalf("NextIn: %v", err)
	}
	if h := got.In(ny).Hour(); h != 9 {
		t.Errorf("fires at %d:00 New York time, want 9:00 (%s)", h, got)
	}
	if want := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("next = %s, want %s", got.UTC(), want)
	}

	// The same spec without a zone stays UTC — the old behavior.
	utcNext, err := Next("0 9 * * *", from)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if want := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC); !utcNext.Equal(want) {
		t.Errorf("utc next = %s, want %s", utcNext, want)
	}
}

// A zoned schedule must survive a DST transition by tracking wall-clock time.
func TestNextInAcrossDST(t *testing.T) {
	// US DST ends 2026-11-01. A 9am-local job on Nov 2 is 14:00 UTC (EST),
	// whereas before the change it would have been 13:00 UTC (EDT).
	from := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	got, err := NextIn("0 9 * * *", "America/New_York", from)
	if err != nil {
		t.Fatalf("NextIn: %v", err)
	}
	if want := time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("post-DST next = %s, want %s", got.UTC(), want)
	}
}

// Descriptors must still work when a zone is applied (the CRON_TZ prefix is
// prepended to them too).
func TestNextInDescriptor(t *testing.T) {
	from := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	got, err := NextIn("@every 1h", "America/New_York", from)
	if err != nil {
		t.Fatalf("NextIn(@every): %v", err)
	}
	if want := from.Add(time.Hour); !got.Equal(want) {
		t.Errorf("next = %s, want %s", got.UTC(), want)
	}
	if _, err := NextIn("@daily", "Europe/Berlin", from); err != nil {
		t.Errorf("@daily with zone: %v", err)
	}
}

func TestValidation(t *testing.T) {
	if !Valid("0 9 * * *") || !Valid("@hourly") {
		t.Error("valid specs rejected")
	}
	if Valid("not a cron") || Valid("") {
		t.Error("invalid specs accepted")
	}
	if !ValidTimezone("") || !ValidTimezone("America/New_York") || !ValidTimezone("UTC") {
		t.Error("valid timezones rejected")
	}
	if ValidTimezone("Mars/Olympus_Mons") {
		t.Error("bogus timezone accepted")
	}
	if _, err := NextIn("0 9 * * *", "Mars/Olympus_Mons", time.Now()); err == nil {
		t.Error("NextIn should reject a bogus timezone")
	}
}
