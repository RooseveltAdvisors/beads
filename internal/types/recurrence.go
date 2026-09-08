package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
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
			return nil, nil
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
	startAt, _, err := parseRecurrenceBound(start, location)
	if err != nil {
		return nil, fmt.Errorf("invalid recurrence_start %q: use YYYY-MM-DD or an ISO-8601 datetime", start)
	}
	if end != "" {
		endAt, dateOnly, err := parseRecurrenceBound(end, location)
		if err != nil {
			return nil, fmt.Errorf("invalid recurrence_end %q: use YYYY-MM-DD or an ISO-8601 datetime", end)
		}
		if dateOnly {
			// A date-only end includes the whole end day, so it compares
			// against the start of the following day, which must still lie
			// strictly after the start for a non-empty window.
			endAt = endAt.AddDate(0, 0, 1)
			if !endAt.After(startAt) {
				return nil, fmt.Errorf("recurrence_end %q is before recurrence_start %q", end, start)
			}
		} else if endAt.Before(startAt) {
			return nil, fmt.Errorf("recurrence_end %q is before recurrence_start %q", end, start)
		}
	}
	return &Recurrence{Schedule: repeat, Start: start, End: end, Timezone: tz}, nil
}

// HasRecurrenceKeys reports whether metadata carries any recurrence contract
// key, whether or not the values form a valid recurrence. Lifecycle paths that
// would otherwise clear an assignee use it to recognize recurrence-bearing
// rows - including rows whose legacy metadata never parses - so the canonical
// owner the contract names survives the whole lifecycle.
func HasRecurrenceKeys(metadata json.RawMessage) bool {
	keys, _ := recurrenceKeyMap(metadata)
	return len(keys) > 0
}

// ParseRecurrenceLenient is the claim-time form of ParseRecurrence: metadata
// that cannot form a valid recurrence (a pre-existing issue's legacy or
// malformed use of the generic keys) is inert, yielding nil exactly like a
// one-off instead of a validation error that would block dispatch. Write
// paths use the strict ParseRecurrence.
func ParseRecurrenceLenient(metadata json.RawMessage, assignee string) *Recurrence {
	recurrence, _ := ParseRecurrence(metadata, assignee)
	return recurrence
}

// RecurrenceKeysEqual reports whether the recurrence metadata keys hold the
// same values in before and after. It distinguishes a write that leaves a
// pre-existing (never-valid) recurrence blob untouched from one that is
// trying to shape the keys into a recurrence.
func RecurrenceKeysEqual(before, after json.RawMessage) bool {
	beforeKeys, okBefore := recurrenceKeyMap(before)
	afterKeys, okAfter := recurrenceKeyMap(after)
	if !okBefore || !okAfter {
		return okBefore == okAfter
	}
	if len(beforeKeys) != len(afterKeys) {
		return false
	}
	for key, beforeRaw := range beforeKeys {
		afterRaw, ok := afterKeys[key]
		if !ok || !rawJSONEqual(beforeRaw, afterRaw) {
			return false
		}
	}
	return true
}

func recurrenceKeyMap(metadata json.RawMessage) (map[string]json.RawMessage, bool) {
	var values map[string]json.RawMessage
	if len(metadata) == 0 || strings.TrimSpace(string(metadata)) == "null" || json.Unmarshal(metadata, &values) != nil {
		return nil, false
	}
	keys := make(map[string]json.RawMessage, 4)
	for _, key := range []string{MetadataRepeat, MetadataRecurrenceStart, MetadataRecurrenceEnd, MetadataRecurrenceTZ} {
		if raw, ok := values[key]; ok {
			keys[key] = raw
		}
	}
	return keys, true
}

func rawJSONEqual(a, b json.RawMessage) bool {
	var left, right interface{}
	if json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil {
		return reflect.DeepEqual(left, right)
	}
	return bytes.Equal(a, b)
}

func parseRecurrenceBound(value string, location *time.Location) (time.Time, bool, error) {
	for _, layout := range []struct {
		pattern  string
		dateOnly bool
	}{{time.RFC3339, false}, {"2006-01-02T15:04:05", false}, {"2006-01-02T15:04", false}, {"2006-01-02", true}} {
		if parsed, err := time.ParseInLocation(layout.pattern, value, location); err == nil {
			return parsed, layout.dateOnly, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("unsupported recurrence bound")
}
