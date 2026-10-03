package cronx

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func mustParse(t *testing.T, expression string) *Schedule {
	t.Helper()
	schedule, err := Parse(expression)
	if err != nil {
		t.Fatalf("parse %q: %v", expression, err)
	}
	return schedule
}

func TestParseRejects(t *testing.T) {
	for _, expression := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 32 * *", "* * * 13 *", "* * * * 8", "a * * * *", "*/0 * * * *", "5-1 * * * *", "1/2/3 * * * *"} {
		if _, err := Parse(expression); err == nil {
			t.Fatalf("expression %q expected error", expression)
		}
	}
}

func TestNextEveryMinute(t *testing.T) {
	schedule := mustParse(t, "* * * * *")
	after := time.Date(2026, 10, 2, 12, 30, 45, 0, time.UTC)
	next, ok := schedule.Next(after, time.UTC)
	if !ok || next.Format(time.RFC3339) != "2026-10-02T12:31:00Z" {
		t.Fatalf("got %v ok=%v", next, ok)
	}
}

func TestNextSpecificTime(t *testing.T) {
	schedule := mustParse(t, "30 9 * * 1-5")
	after := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) // Friday
	next, ok := schedule.Next(after, time.UTC)
	if !ok || next.Format(time.RFC3339) != "2026-10-05T09:30:00Z" {
		t.Fatalf("expected Monday 09:30, got %v ok=%v", next, ok)
	}
}

func TestNextStepAndList(t *testing.T) {
	schedule := mustParse(t, "*/15 0 1,15 * *")
	after := time.Date(2026, 10, 1, 0, 7, 0, 0, time.UTC)
	next, ok := schedule.Next(after, time.UTC)
	if !ok || next.Format(time.RFC3339) != "2026-10-01T00:15:00Z" {
		t.Fatalf("got %v ok=%v", next, ok)
	}
}

func TestNextDayMonthWeekdayOr(t *testing.T) {
	// Both day fields restricted: the 13th is a Tuesday in October 2026, so
	// "13th or Tuesday" first fires on Tue Oct 13 for a search from Oct 2.
	schedule := mustParse(t, "0 0 13 * 2")
	after := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	next, ok := schedule.Next(after, time.UTC)
	if !ok || next.Format(time.RFC3339) != "2026-10-06T00:00:00Z" {
		t.Fatalf("got %v ok=%v", next, ok)
	}
}

func TestNextImpossibleDate(t *testing.T) {
	schedule := mustParse(t, "0 0 31 2 *")
	if _, ok := schedule.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); ok {
		t.Fatal("february 31st must never match")
	}
}

func TestNextSkipsNonexistentLocalTime(t *testing.T) {
	// America/New_York springs forward at 2026-03-08 02:00 -> 03:00; the local
	// times 02:00-02:59 do not exist and must be skipped.
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	schedule := mustParse(t, "30 2 8 3 *")
	after := time.Date(2026, 3, 7, 12, 0, 0, 0, location)
	next, ok := schedule.Next(after, location)
	if !ok {
		t.Fatal("expected a match in 2027")
	}
	if next.Year() != 2027 || next.Month() != time.March {
		t.Fatalf("expected 2027-03-08 02:30 to be skipped to 2027, got %v", next)
	}
}

func TestNextFiresOnceForRepeatedLocalTime(t *testing.T) {
	// America/New_York falls back on 2026-11-01: 01:30 local happens twice.
	// The schedule must produce exactly one trigger instant for that day.
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	schedule := mustParse(t, "30 1 1 11 *")
	after := time.Date(2026, 10, 30, 0, 0, 0, 0, location)
	first, ok := schedule.Next(after, location)
	if !ok {
		t.Fatal("expected first match")
	}
	second, ok := schedule.Next(first, location)
	if !ok {
		t.Fatal("expected second match")
	}
	if first.UTC().Format(time.RFC3339) != "2026-11-01T05:30:00Z" {
		t.Fatalf("first occurrence %v unexpected", first.UTC())
	}
	if second.Year() != 2027 {
		t.Fatalf("repeated wall clock must fire once, next is %v", second)
	}
}

func TestNextTimezoneOffset(t *testing.T) {
	location, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	schedule := mustParse(t, "0 9 * * *")
	after := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	next, ok := schedule.Next(after, location)
	if !ok || next.UTC().Format(time.RFC3339) != "2026-10-02T01:00:00Z" {
		t.Fatalf("got %v ok=%v", next.UTC(), ok)
	}
}
