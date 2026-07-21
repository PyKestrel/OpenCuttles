package main

import (
	"testing"
	"time"
)

// noJitter makes nextBackoff deterministic: rnd()=0.5 maps to a jitter factor
// of exactly 1.0, so the doubling is testable on its own.
func noJitter() float64 { return 0.5 }

func TestNextBackoffDoublesAndCaps(t *testing.T) {
	got := backoffMin
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, backoffMax, backoffMax}

	for i, expect := range want {
		got = nextBackoff(got, noJitter)
		if got != expect {
			t.Fatalf("step %d: got %s, want %s", i+1, got, expect)
		}
	}
}

// Jitter is the whole point — without it a restarted appliance brings every
// runner back in lockstep, and they stay synchronized.
func TestNextBackoffAppliesJitterWithinBounds(t *testing.T) {
	base := 10 * time.Second

	// Extremes of rnd's [0,1) range map to ±20%.
	if low := nextBackoff(base, func() float64 { return 0 }); low != 16*time.Second {
		t.Fatalf("min jitter = %s, want 16s (20s -20%%)", low)
	}
	if high := nextBackoff(base, func() float64 { return 0.999999 }); high < 23*time.Second || high > 24*time.Second {
		t.Fatalf("max jitter = %s, want ~24s (20s +20%%)", high)
	}
}

func TestNextBackoffStaysWithinBounds(t *testing.T) {
	// Sweep the whole rnd range from several starting points and assert the
	// result is always usable — never zero, never unbounded.
	for _, start := range []time.Duration{0, backoffMin, 7 * time.Second, backoffMax, 10 * time.Minute} {
		for i := 0; i <= 20; i++ {
			r := float64(i) / 20
			got := nextBackoff(start, func() float64 { return r })
			if got < backoffMin {
				t.Fatalf("start=%s rnd=%.2f gave %s, below the %s floor", start, r, got, backoffMin)
			}
			if got > backoffMax {
				t.Fatalf("start=%s rnd=%.2f gave %s, above the %s cap", start, r, got, backoffMax)
			}
		}
	}
}

// A tunnel that survived backoffReset is evidence the appliance is healthy, so
// the next blip should retry quickly instead of inheriting a long delay. This
// pins the constants' relationship rather than the loop itself, which needs
// real time to exercise.
func TestBackoffConstantsAreSane(t *testing.T) {
	if backoffMin <= 0 {
		t.Fatal("backoffMin must be positive or the loop spins")
	}
	if backoffMax <= backoffMin {
		t.Fatal("backoffMax must exceed backoffMin")
	}
	if backoffReset < backoffMax/2 {
		t.Fatalf("backoffReset (%s) is short relative to backoffMax (%s): a flapping "+
			"tunnel would keep resetting to the floor and hammer the appliance",
			backoffReset, backoffMax)
	}
}
