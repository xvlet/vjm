package threadgroup

import (
	"math"
	"math/rand/v2"
	"time"
)

// PoissonPacer models randomized arrivals with a mean rate, adding jitter to a constant rate
type PoissonPacer struct {
	Freq float64
	Per  time.Duration
}

// Pace returns the absolute duration to wait before the next request.
func (p PoissonPacer) Pace(elapsed time.Duration, hits uint64) (time.Duration, bool) {
	if p.Freq <= 0 {
		return 0, true // Stop if freq is 0 or negative
	}

	// Base expected time for the NEXT hit
	expected := time.Duration(float64(hits+1) * float64(p.Per) / p.Freq)

	// Exponential distribution: -ln(1-U)/lambda
	u := 1.0 - rand.Float64()
	jitter := -math.Log(u) * float64(p.Per) / p.Freq

	delta := expected + time.Duration(jitter)
	if delta <= elapsed {
		return 0, false
	}
	return delta - elapsed, false
}

// Rate returns the overall rate of the pacer in hits per second.
func (p PoissonPacer) Rate(elapsed time.Duration) float64 {
	return p.Freq / p.Per.Seconds()
}
