//go:build integration

package harness

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// Timeout tiers. Pick by what is being waited on rather than by guessing a
// duration: Short for API-level effects, Medium for config reaching pods and
// streams appearing, Long for StatefulSet rollouts and certificate issuance.
//
// IT_TIMEOUT_SCALE multiplies all three, for slow CI runners.
var (
	Short  = scaled(30 * time.Second)
	Medium = scaled(2 * time.Minute)
	Long   = scaled(5 * time.Minute)
)

// DefaultInterval is the poll interval used by the WaitX helpers.
const DefaultInterval = 500 * time.Millisecond

func scaled(d time.Duration) time.Duration {
	s := os.Getenv("IT_TIMEOUT_SCALE")
	if s == "" {
		return d
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return d
	}
	return time.Duration(float64(d) * f)
}

// WaitFor polls fn until it reports true, or fails the test.
//
// fn returns the current observation alongside the verdict; that observation is
// what gets reported on timeout, which is the difference between a failure you
// can diagnose and one you have to reproduce.
func WaitFor(t *testing.T, timeout, interval time.Duration, desc string, fn func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		ok, observed := fn()
		if ok {
			return
		}
		last = observed
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(interval)
	}
	t.Fatalf("timed out after %s waiting for %s; last observed: %s", timeout, desc, last)
}

// Wait is WaitFor with the default interval.
func Wait(t *testing.T, timeout time.Duration, desc string, fn func() (bool, string)) {
	t.Helper()
	WaitFor(t, timeout, DefaultInterval, desc, fn)
}

// WaitInt polls until fn returns want.
func WaitInt(t *testing.T, timeout time.Duration, desc string, want int, fn func() int) {
	t.Helper()
	Wait(t, timeout, desc, func() (bool, string) {
		got := fn()
		return got == want, "want " + strconv.Itoa(want) + ", got " + strconv.Itoa(got)
	})
}

// Consistently asserts fn stays true for the whole duration.
//
// As important as WaitFor: plenty of operator bugs settle correctly and then
// oscillate, and a single-shot check cannot tell the two apart.
func Consistently(t *testing.T, duration, interval time.Duration, desc string, fn func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if ok, observed := fn(); !ok {
			t.Fatalf("%s did not hold: %s", desc, observed)
		}
		time.Sleep(interval)
	}
}
