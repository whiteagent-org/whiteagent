package cron

import (
	"testing"
	"time"
)

func TestParseValid(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		minute []int
		hour   []int
		dom    []int
		month  []int
		dow    []int
	}{
		{
			name:   "every 15 minutes",
			expr:   "*/15 * * * *",
			minute: []int{0, 15, 30, 45},
			hour:   seq(0, 23),
			dom:    seq(1, 31),
			month:  seq(1, 12),
			dow:    seq(0, 6),
		},
		{
			name:   "weekdays at 9am",
			expr:   "0 9 * * 1-5",
			minute: []int{0},
			hour:   []int{9},
			dom:    seq(1, 31),
			month:  seq(1, 12),
			dow:    []int{1, 2, 3, 4, 5},
		},
		{
			name:   "lists",
			expr:   "30 8,12 1,15 * *",
			minute: []int{30},
			hour:   []int{8, 12},
			dom:    []int{1, 15},
			month:  seq(1, 12),
			dow:    seq(0, 6),
		},
		{
			name:   "midnight daily",
			expr:   "0 0 * * *",
			minute: []int{0},
			hour:   []int{0},
			dom:    seq(1, 31),
			month:  seq(1, 12),
			dow:    seq(0, 6),
		},
		{
			name:   "step on range",
			expr:   "1-30/5 * * * *",
			minute: []int{1, 6, 11, 16, 21, 26},
			hour:   seq(0, 23),
			dom:    seq(1, 31),
			month:  seq(1, 12),
			dow:    seq(0, 6),
		},
		{
			name:   "single values",
			expr:   "5 10 15 6 3",
			minute: []int{5},
			hour:   []int{10},
			dom:    []int{15},
			month:  []int{6},
			dow:    []int{3},
		},
		{
			name:   "step on wildcard hour",
			expr:   "0 */6 * * *",
			minute: []int{0},
			hour:   []int{0, 6, 12, 18},
			dom:    seq(1, 31),
			month:  seq(1, 12),
			dow:    seq(0, 6),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.expr, err)
			}
			assertSlice(t, "Minute", c.Minute, tt.minute)
			assertSlice(t, "Hour", c.Hour, tt.hour)
			assertSlice(t, "DOM", c.DOM, tt.dom)
			assertSlice(t, "Month", c.Month, tt.month)
			assertSlice(t, "DOW", c.DOW, tt.dow)
		})
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty", ""},
		{"too few fields", "* * *"},
		{"too many fields", "* * * * * *"},
		{"minute out of range", "60 * * * *"},
		{"hour out of range", "* 24 * * *"},
		{"dom out of range", "* * 0 * *"},
		{"dom too high", "* * 32 * *"},
		{"month out of range", "* * * 13 *"},
		{"month zero", "* * * 0 *"},
		{"dow out of range", "* * * * 7"},
		{"non-numeric", "abc * * * *"},
		{"bad range", "5-2 * * * *"},
		{"step zero", "*/0 * * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			if err == nil {
				t.Fatalf("Parse(%q) expected error, got nil", tt.expr)
			}
		})
	}
}

func TestNextAfter(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		after  time.Time
		expect time.Time
	}{
		{
			name:   "every 15 min advance",
			expr:   "*/15 * * * *",
			after:  time.Date(2025, 1, 1, 10, 3, 0, 0, time.UTC),
			expect: time.Date(2025, 1, 1, 10, 15, 0, 0, time.UTC),
		},
		{
			name:   "weekdays skip to next day",
			expr:   "0 9 * * 1-5",
			after:  time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC), // Monday 09:00
			expect: time.Date(2025, 1, 7, 9, 0, 0, 0, time.UTC), // Tuesday 09:00
		},
		{
			name:   "monthly first day",
			expr:   "0 0 1 * *",
			after:  time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			expect: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "year rollover",
			expr:   "0 0 1 1 *",
			after:  time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
			expect: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "exact match advances",
			expr:   "0 0 * * *",
			after:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			expect: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "every 15 min at boundary",
			expr:   "*/15 * * * *",
			after:  time.Date(2025, 1, 1, 10, 15, 0, 0, time.UTC),
			expect: time.Date(2025, 1, 1, 10, 30, 0, 0, time.UTC),
		},
		{
			name:   "hour rollover",
			expr:   "0 * * * *",
			after:  time.Date(2025, 1, 1, 23, 0, 0, 0, time.UTC),
			expect: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "month boundary feb to mar",
			expr:   "0 0 * * *",
			after:  time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC),
			expect: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "leap year feb 29",
			expr:   "0 0 29 2 *",
			after:  time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC),
			expect: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.expr, err)
			}
			got := c.NextAfter(tt.after)
			if !got.Equal(tt.expect) {
				t.Fatalf("NextAfter(%v) = %v, want %v", tt.after, got, tt.expect)
			}
		})
	}
}

func TestNextAfterDOMDOWOrSemantics(t *testing.T) {
	// "0 0 15 * 1" means fire on 15th OR on Mondays
	c, err := Parse("0 0 15 * 1")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// 2025-01-13 is Monday, 2025-01-15 is Wednesday
	// After Jan 12 (Sunday), next should be Jan 13 (Monday) not Jan 15 (15th)
	after := time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC)
	got := c.NextAfter(after)
	expect := time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC)
	if !got.Equal(expect) {
		t.Fatalf("DOM/DOW OR: NextAfter(%v) = %v, want %v", after, got, expect)
	}

	// After Jan 13 (Monday), next should be Jan 15 (15th, Wednesday)
	after2 := time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC)
	got2 := c.NextAfter(after2)
	expect2 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	if !got2.Equal(expect2) {
		t.Fatalf("DOM/DOW OR: NextAfter(%v) = %v, want %v", after2, got2, expect2)
	}

	// After Jan 15 (Wed), next should be Jan 20 (Monday)
	after3 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	got3 := c.NextAfter(after3)
	expect3 := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)
	if !got3.Equal(expect3) {
		t.Fatalf("DOM/DOW OR: NextAfter(%v) = %v, want %v", after3, got3, expect3)
	}
}

// helpers

func seq(from, to int) []int {
	s := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		s = append(s, i)
	}
	return s
}

func assertSlice(t *testing.T, name string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len=%d, want %d\n  got:  %v\n  want: %v", name, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %d, want %d\n  got:  %v\n  want: %v", name, i, got[i], want[i], got, want)
		}
	}
}
