package discovery

import (
	"math/rand/v2"
	"time"
)

const (
	// backoffCap bounds the retry backoff as a multiple of the interval.
	backoffCap = 10
	// jitterFraction is the largest share of an interval added as jitter.
	jitterFraction = 0.10
)

// RetryAfter is the delay before the next run after `failures` consecutive
// failures: interval, doubling each time, capped at ten times the interval.
// There is no separate retry configuration; this shape fits every interval.
func RetryAfter(interval time.Duration, failures int32) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := interval
	for i := int32(1); i < failures && d < interval*backoffCap; i++ {
		d *= 2
	}
	if d > interval*backoffCap {
		d = interval * backoffCap
	}
	return d
}

// Jitter adds up to 10% of d, so sources created together drift apart
// instead of polling in lockstep. random must return [0,1); nil uses the
// default source.
func Jitter(d time.Duration, random func() float64) time.Duration {
	if random == nil {
		random = rand.Float64
	}
	return d + time.Duration(float64(d)*jitterFraction*random())
}
