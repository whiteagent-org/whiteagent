// Package cron provides a standard 5-field cron expression parser
// and next-fire-time computation.
package cron

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cron holds the parsed fields of a cron expression.
// Each field is a sorted slice of valid values.
type Cron struct {
	Minute []int
	Hour   []int
	DOM    []int // day of month (1-31)
	Month  []int // (1-12)
	DOW    []int // day of week (0-6, 0=Sunday)

	// domWild and dowWild track whether the original expression
	// used a wildcard for DOM / DOW. This controls OR semantics.
	domWild bool
	dowWild bool
}

// Parse parses a standard 5-field cron expression
// (minute hour day-of-month month day-of-week).
func Parse(expr string) (Cron, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return Cron{}, fmt.Errorf("cron: empty expression")
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Cron{}, fmt.Errorf("cron: expected 5 fields, got %d", len(fields))
	}

	var c Cron
	var err error

	c.Minute, err = parseField(fields[0], 0, 59)
	if err != nil {
		return Cron{}, fmt.Errorf("cron: minute: %w", err)
	}
	c.Hour, err = parseField(fields[1], 0, 23)
	if err != nil {
		return Cron{}, fmt.Errorf("cron: hour: %w", err)
	}
	c.DOM, err = parseField(fields[2], 1, 31)
	if err != nil {
		return Cron{}, fmt.Errorf("cron: day-of-month: %w", err)
	}
	c.Month, err = parseField(fields[3], 1, 12)
	if err != nil {
		return Cron{}, fmt.Errorf("cron: month: %w", err)
	}
	c.DOW, err = parseField(fields[4], 0, 6)
	if err != nil {
		return Cron{}, fmt.Errorf("cron: day-of-week: %w", err)
	}

	c.domWild = isWild(fields[2], 1, 31)
	c.dowWild = isWild(fields[4], 0, 6)

	return c, nil
}

// isWild returns true if the field expression expands to all values in [lo..hi].
func isWild(field string, lo, hi int) bool {
	vals, err := parseField(field, lo, hi)
	if err != nil {
		return false
	}
	return len(vals) == hi-lo+1
}

// parseField parses a single cron field (supports *, N, N-M, */S, N-M/S, and comma-separated lists).
func parseField(field string, lo, hi int) ([]int, error) {
	// handle comma-separated list
	if strings.Contains(field, ",") {
		set := map[int]bool{}
		for _, part := range strings.Split(field, ",") {
			vals, err := parseField(part, lo, hi)
			if err != nil {
				return nil, err
			}
			for _, v := range vals {
				set[v] = true
			}
		}
		return sortedKeys(set), nil
	}

	// handle step: base/step
	if strings.Contains(field, "/") {
		parts := strings.SplitN(field, "/", 2)
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid step %q", parts[1])
		}
		start, end := lo, hi
		if parts[0] != "*" {
			start, end, err = parseRange(parts[0], lo, hi)
			if err != nil {
				return nil, err
			}
		}
		var vals []int
		for v := start; v <= end; v += step {
			vals = append(vals, v)
		}
		return vals, nil
	}

	// wildcard
	if field == "*" {
		vals := make([]int, 0, hi-lo+1)
		for v := lo; v <= hi; v++ {
			vals = append(vals, v)
		}
		return vals, nil
	}

	// range
	if strings.Contains(field, "-") {
		start, end, err := parseRange(field, lo, hi)
		if err != nil {
			return nil, err
		}
		vals := make([]int, 0, end-start+1)
		for v := start; v <= end; v++ {
			vals = append(vals, v)
		}
		return vals, nil
	}

	// single value
	v, err := strconv.Atoi(field)
	if err != nil {
		return nil, fmt.Errorf("invalid value %q", field)
	}
	if v < lo || v > hi {
		return nil, fmt.Errorf("value %d out of range [%d-%d]", v, lo, hi)
	}
	return []int{v}, nil
}

// parseRange parses "N-M" and validates bounds.
func parseRange(s string, lo, hi int) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range %q", s)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range start %q", parts[0])
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid range end %q", parts[1])
	}
	if start < lo || end > hi {
		return 0, 0, fmt.Errorf("range %d-%d out of bounds [%d-%d]", start, end, lo, hi)
	}
	if start > end {
		return 0, 0, fmt.Errorf("range start %d > end %d", start, end)
	}
	return start, end, nil
}

func sortedKeys(m map[int]bool) []int {
	s := make([]int, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Ints(s)
	return s
}

// contains returns true if sorted slice s contains v.
func contains(s []int, v int) bool {
	i := sort.SearchInts(s, v)
	return i < len(s) && s[i] == v
}

// nextGE returns the smallest value in sorted s >= v, or -1 if none.
func nextGE(s []int, v int) int {
	i := sort.SearchInts(s, v)
	if i < len(s) {
		return s[i]
	}
	return -1
}

// maxDays returns the number of days in the given month/year.
func maxDays(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// NextAfter returns the next time the cron expression fires strictly after t.
// It searches up to 4 years ahead. If no match is found, it returns the zero time.
func (c Cron) NextAfter(t time.Time) time.Time {
	// Start one minute after t, truncated to the minute.
	cur := t.Truncate(time.Minute).Add(time.Minute)

	year := cur.Year()
	limit := year + 4

	for year <= limit {
		for _, mon := range c.Month {
			m := time.Month(mon)
			if year == cur.Year() && m < cur.Month() {
				continue
			}

			md := maxDays(year, m)

			for day := 1; day <= md; day++ {
				if year == cur.Year() && m == cur.Month() && day < cur.Day() {
					continue
				}

				if !c.dayMatches(day, year, m) {
					continue
				}

				for _, hr := range c.Hour {
					if year == cur.Year() && m == cur.Month() && day == cur.Day() && hr < cur.Hour() {
						continue
					}

					for _, min := range c.Minute {
						if year == cur.Year() && m == cur.Month() && day == cur.Day() && hr == cur.Hour() && min < cur.Minute() {
							continue
						}

						candidate := time.Date(year, m, day, hr, min, 0, 0, t.Location())
						// Verify the date is valid (handles e.g., Feb 30 → Mar 2 normalization).
						if candidate.Month() != m || candidate.Day() != day {
							continue
						}
						return candidate
					}
				}
			}
		}
		year++
	}

	return time.Time{}
}

// dayMatches checks whether a given day satisfies the DOM/DOW constraints,
// implementing OR semantics when both are non-wildcard.
func (c Cron) dayMatches(day, year int, month time.Month) bool {
	domMatch := contains(c.DOM, day)
	dow := int(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday())
	dowMatch := contains(c.DOW, dow)

	if c.domWild && c.dowWild {
		return true // both wildcard: any day
	}
	if c.domWild {
		return dowMatch // only DOW constrains
	}
	if c.dowWild {
		return domMatch // only DOM constrains
	}
	// Both non-wildcard: OR semantics
	return domMatch || dowMatch
}
