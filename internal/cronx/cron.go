// Package cronx parses standard five-field cron expressions and computes the
// next execution time in a given IANA timezone. Local times skipped by a
// daylight-saving transition never fire; ambiguous local times fire once.
package cronx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	// Bare-metal deployments may lack system tzdata; embed the database so
	// IANA timezone loading never fails at schedule time.
	_ "time/tzdata"
)

const searchYears = 5

type Schedule struct {
	minutes          [60]bool
	hours            [24]bool
	days             [32]bool
	months           [13]bool
	weekdays         [7]bool
	dayRestricted    bool
	weekdayRestricted bool
}

// Parse accepts "minute hour day-of-month month day-of-week" with "*", lists,
// ranges and step values. Day-of-week runs 0-7 where both 0 and 7 mean Sunday.
func Parse(expression string) (*Schedule, error) {
	fields := strings.Fields(strings.TrimSpace(expression))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have five fields")
	}
	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute field: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour field: %w", err)
	}
	days, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month field: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month field: %w", err)
	}
	weekdays, err := parseField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week field: %w", err)
	}
	schedule := &Schedule{
		dayRestricted:    !isWildcard(fields[2]),
		weekdayRestricted: !isWildcard(fields[4]),
	}
	for _, value := range minutes {
		schedule.minutes[value] = true
	}
	for _, value := range hours {
		schedule.hours[value] = true
	}
	for _, value := range days {
		schedule.days[value] = true
	}
	for _, value := range months {
		schedule.months[value] = true
	}
	for _, value := range weekdays {
		schedule.weekdays[value%7] = true
	}
	return schedule, nil
}

// IntervalDuration maps the four fixed interval labels used by sync tasks and
// workflows to their duration; unknown labels return 0.
func IntervalDuration(value string) time.Duration {
	switch strings.TrimSpace(value) {
	case "每 15 分钟":
		return 15 * time.Minute
	case "每 30 分钟":
		return 30 * time.Minute
	case "每小时":
		return time.Hour
	case "每天":
		return 24 * time.Hour
	default:
		return 0
	}
}

// LoadTimezone parses an IANA timezone name with the embedded tzdata fallback.
func LoadTimezone(name string) (*time.Location, error) {
	if strings.TrimSpace(name) == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(strings.TrimSpace(name))
}

// Advance returns the next schedule time strictly after "from", skipping
// every occurrence missed before "now": downtime does not replay missed
// triggers.
func (s *Schedule) Advance(from, now time.Time, loc *time.Location) (time.Time, error) {
	next, ok := s.Next(from, loc)
	if !ok {
		return time.Time{}, fmt.Errorf("cron expression has no future occurrence")
	}
	for iterations := 0; !next.After(now); iterations++ {
		if iterations > 100000 {
			return time.Time{}, fmt.Errorf("cron catch-up exceeded iteration budget")
		}
		next, ok = s.Next(next, loc)
		if !ok {
			return time.Time{}, fmt.Errorf("cron expression has no future occurrence")
		}
	}
	return next, nil
}

// AdvanceInterval is Advance for the four fixed interval labels.
func AdvanceInterval(label string, from, now time.Time) (time.Time, error) {
	duration := IntervalDuration(label)
	if duration <= 0 {
		return time.Time{}, fmt.Errorf("unknown interval label %q", label)
	}
	next := from.Add(duration)
	if !next.After(now) {
		steps := int(now.Sub(next)/duration) + 1
		next = next.Add(time.Duration(steps) * duration)
	}
	return next, nil
}

func isWildcard(field string) bool { return field == "*" || field == "*/1" }

// parseField expands one cron field into its matching integer values.
func parseField(field string, min, max int) ([]int, error) {
	if field == "" {
		return nil, fmt.Errorf("empty field")
	}
	var values []int
	for _, part := range strings.Split(field, ",") {
		step := 1
		rangeText := part
		if slash := strings.IndexByte(part, '/'); slash >= 0 {
			rangeText = part[:slash]
			parsed, err := strconv.Atoi(part[slash+1:])
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("invalid step %q", part[slash+1:])
			}
			step = parsed
		}
		start, end := min, max
		if rangeText != "*" {
			dash := strings.IndexByte(rangeText, '-')
			if dash < 0 {
				value, err := strconv.Atoi(rangeText)
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", rangeText)
				}
				if value < min || value > max {
					return nil, fmt.Errorf("value %d out of range %d-%d", value, min, max)
				}
				start, end = value, value
			} else {
				first, errFirst := strconv.Atoi(rangeText[:dash])
				last, errLast := strconv.Atoi(rangeText[dash+1:])
				if errFirst != nil || errLast != nil {
					return nil, fmt.Errorf("invalid range %q", rangeText)
				}
				if first < min || last > max || first > last {
					return nil, fmt.Errorf("range %q out of bounds %d-%d", rangeText, min, max)
				}
				start, end = first, last
			}
		}
		for value := start; value <= end; value += step {
			values = append(values, value)
		}
	}
	return values, nil
}

// Next returns the first matching instant strictly after the given time,
// evaluated on wall-clock time in loc. It reports false when no match exists
// within the search window (for example "0 0 31 2 *").
func (s *Schedule) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	reference := after.In(loc)
	day := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; i < searchYears*366; i++ {
		year, month, number := day.Date()
		if s.months[int(month)] && s.dayMatches(year, month, number) {
			for hour := 0; hour < 24; hour++ {
				if !s.hours[hour] {
					continue
				}
				for minute := 0; minute < 60; minute++ {
					if !s.minutes[minute] {
						continue
					}
					candidate := time.Date(year, month, number, hour, minute, 0, 0, loc)
					if !candidate.After(reference) {
						continue
					}
					// A local time inside a spring-forward gap normalizes to a
					// different wall clock; those local times do not exist.
					if candidate.Hour() != hour || candidate.Minute() != minute ||
						candidate.Day() != number || candidate.Month() != month {
						continue
					}
					return candidate, true
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

func (s *Schedule) dayMatches(year int, month time.Month, day int) bool {
	dayOK := s.days[day]
	// Vixie-cron semantics: when both day-of-month and day-of-week are
	// restricted, either may match; otherwise both must match.
	weekday := int(time.Date(year, month, day, 12, 0, 0, 0, time.UTC).Weekday())
	weekdayOK := s.weekdays[weekday]
	if s.dayRestricted && s.weekdayRestricted {
		return dayOK || weekdayOK
	}
	return dayOK && weekdayOK
}
