package analytics

import (
	"fmt"
	"net/http"
	"time"

	// Blank-imported so time.LoadLocation("Africa/Johannesburg") resolves
	// even on a machine with no system IANA tzdata installed (e.g. a bare
	// Windows dev box) — embeds the tz database into the binary instead of
	// relying on the OS to provide one.
	_ "time/tzdata"
)

// timeZone is the ONE fixed timezone this MVP's "today"/date-range bounding
// uses, per the stage brief's requirement to document it explicitly. All
// owner-analytics date arithmetic (default "today", date-only ?from=/&to=
// bounds, and the revenue-vs-fuel daily series buckets) is anchored to this
// zone, not server-local time or UTC. A future multi-region deployment would
// need to make this configurable; hardcoded is the documented MVP choice.
var timeZone = mustLoadLocation("Africa/Johannesburg")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("analytics: failed to load timezone %q: %v", name, err))
	}
	return loc
}

const dateOnlyLayout = "2006-01-02"

// parseDateRange resolves the ?from=&to= query params into a [from, to)
// half-open interval.
//
//   - Missing `from`: defaults to the start of today in timeZone.
//   - Missing `to`: defaults to "now".
//   - A date-only value ("2006-01-02") is interpreted as midnight of that
//     date in timeZone; for `to` specifically, a date-only value is bumped
//     to midnight of the NEXT day, so a plain "?to=2026-07-14" includes the
//     whole of the 14th rather than excluding it at its very first instant.
//   - A full RFC3339 timestamp is used exactly as given (already
//     unambiguous about its own offset).
//
// This is the one documented "today" boundary the stage brief asks for.
func parseDateRange(r *http.Request) (from, to time.Time, err error) {
	now := time.Now().In(timeZone)

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr == "" {
		from = startOfDay(now)
	} else {
		from, err = parseDateBound(fromStr, false)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from: %w", err)
		}
	}

	if toStr == "" {
		to = now
	} else {
		to, err = parseDateBound(toStr, true)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to: %w", err)
		}
	}

	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to must be after from")
	}

	return from, to, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, timeZone)
}

func parseDateBound(s string, isUpperBound bool) (time.Time, error) {
	if t, err := time.Parse(dateOnlyLayout, s); err == nil {
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, timeZone)
		if isUpperBound {
			t = t.AddDate(0, 0, 1)
		}
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("must be YYYY-MM-DD or RFC3339, got %q", s)
}
