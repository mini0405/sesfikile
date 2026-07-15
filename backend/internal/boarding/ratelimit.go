package boarding

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// codeRateLimiter throttles short-code scan attempts per driver — a simple
// in-memory fixed-window counter, sufficient at this MVP's scale (one
// process, no horizontal scaling). It exists because an 8-character code is
// short enough that unlimited guessing would matter; a 1.1e12-combination
// space becomes infeasible to brute-force only when combined with both the
// ~3 minute pass TTL AND a cap on how many attempts a single driver account
// can make per minute (see docs/PROGRESS.md for the full reasoning).
//
// Deliberately per-driver, not per-code: a code is looked up by exact value
// (no partial-match oracle), so the meaningful attacker budget is "how many
// distinct codes can one authenticated driver account try before a live
// code's TTL expires," not "how many times can this one code be retried."
type codeRateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	max      int
	attempts map[uuid.UUID][]time.Time
}

func newCodeRateLimiter(window time.Duration, max int) *codeRateLimiter {
	return &codeRateLimiter{
		window:   window,
		max:      max,
		attempts: make(map[uuid.UUID][]time.Time),
	}
}

// Allow records an attempt for driverID and reports whether it's within the
// limit. Old attempts outside the window are pruned on every call, so the
// map never grows unbounded for a long-lived process.
func (l *codeRateLimiter) Allow(driverID uuid.UUID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	kept := l.attempts[driverID][:0]
	for _, t := range l.attempts[driverID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= l.max {
		l.attempts[driverID] = kept
		return false
	}

	l.attempts[driverID] = append(kept, now)
	return true
}
