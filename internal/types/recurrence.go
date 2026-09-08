package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	MetadataRepeat          = "repeat"
	MetadataRecurrenceStart = "recurrence_start"
	MetadataRecurrenceEnd   = "recurrence_end"
	MetadataRecurrenceTZ    = "recurrence_tz"
)

// Recurrence is the validated scheduling contract stored in issue metadata.
// Execution remains the responsibility of schedulers outside beads.
type Recurrence struct {
	Schedule string
	Start    string
	End      string
	Timezone string
}

// ParseRecurrence validates and returns recurrence metadata. A nil result means
// the issue is an ordinary one-off with none of the recurrence keys present.
func ParseRecurrence(metadata json.RawMessage, assignee string) (*Recurrence, error) {
	var values map[string]json.RawMessage
	if len(metadata) > 0 && strings.TrimSpace(string(metadata)) != "null" {
		if err := json.Unmarshal(metadata, &values); err != nil {
			// Arbitrary non-object metadata remains valid for one-off issues.
			var objectType map[string]any
			if json.Unmarshal(metadata, &objectType) != nil {
				return nil, nil
			}
			return nil, err
		}
	}

	read := func(key string) (string, bool, error) {
		raw, ok := values[key]
		if !ok {
			return "", false, nil
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", true, fmt.Errorf("recurrence metadata %q must be a string", key)
		}
		return strings.TrimSpace(value), true, nil
	}

	repeat, hasRepeat, err := read(MetadataRepeat)
	if err != nil {
		return nil, err
	}
	start, hasStart, err := read(MetadataRecurrenceStart)
	if err != nil {
		return nil, err
	}
	end, hasEnd, err := read(MetadataRecurrenceEnd)
	if err != nil {
		return nil, err
	}
	tz, hasTZ, err := read(MetadataRecurrenceTZ)
	if err != nil {
		return nil, err
	}
	if !hasRepeat {
		if hasStart || hasEnd || hasTZ {
			return nil, fmt.Errorf("recurrence metadata requires %q", MetadataRepeat)
		}
		return nil, nil
	}
	if repeat == "" {
		return nil, fmt.Errorf("recurrence metadata %q must not be empty", MetadataRepeat)
	}
	if !hasStart || start == "" {
		return nil, fmt.Errorf("recurrence metadata requires %q", MetadataRecurrenceStart)
	}
	if !hasTZ || tz == "" {
		return nil, fmt.Errorf("recurrence metadata requires %q", MetadataRecurrenceTZ)
	}
	if hasEnd && end == "" {
		return nil, fmt.Errorf("recurrence metadata %q must not be empty; omit it for no end", MetadataRecurrenceEnd)
	}
	if strings.TrimSpace(assignee) == "" {
		return nil, fmt.Errorf("recurring issues require an assignee naming the canonical owning agent")
	}

	if repeat != "daily" && repeat != "weekly" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(repeat); err != nil {
			return nil, fmt.Errorf("invalid recurrence schedule %q: use daily, weekly, or a five-field cron expression: %w", repeat, err)
		}
	}
	if tz == "Local" {
		return nil, fmt.Errorf("invalid recurrence timezone %q: use an explicit IANA timezone", tz)
	}
	location, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid recurrence timezone %q: use an IANA timezone", tz)
	}
	startAt, err := parseRecurrenceBound(start, location)
	if err != nil {
		return nil, fmt.Errorf("invalid recurrence_start %q: use YYYY-MM-DD or an ISO-8601 datetime", start)
	}
	if end != "" {
		endAt, err := parseRecurrenceBound(end, location)
		if err != nil {
			return nil, fmt.Errorf("invalid recurrence_end %q: use YYYY-MM-DD or an ISO-8601 datetime", end)
		}
		if endAt.Before(startAt) {
			return nil, fmt.Errorf("recurrence_end %q is before recurrence_start %q", end, start)
		}
	}
	return &Recurrence{Schedule: repeat, Start: start, End: end, Timezone: tz}, nil
}

func parseRecurrenceBound(value string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported recurrence bound")
}
