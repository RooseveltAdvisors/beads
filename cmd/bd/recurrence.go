package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var recurrenceFlags = []struct {
	flag string
	key  string
}{
	{"repeat", types.MetadataRepeat},
	{"recurrence-start", types.MetadataRecurrenceStart},
	{"recurrence-end", types.MetadataRecurrenceEnd},
	{"recurrence-tz", types.MetadataRecurrenceTZ},
}

func applyCreateRecurrenceFlags(cmd *cobra.Command, metadata json.RawMessage, assignee string) (json.RawMessage, error) {
	changed := false
	for _, field := range recurrenceFlags {
		changed = changed || cmd.Flags().Changed(field.flag)
	}
	if !changed {
		if _, err := types.ParseRecurrence(metadata, assignee); err != nil {
			return nil, err
		}
		return metadata, nil
	}

	values := map[string]json.RawMessage{}
	if len(metadata) > 0 && strings.TrimSpace(string(metadata)) != "null" {
		if err := json.Unmarshal(metadata, &values); err != nil {
			return nil, fmt.Errorf("recurrence flags require metadata to be a JSON object: %w", err)
		}
	}
	for _, field := range recurrenceFlags {
		if !cmd.Flags().Changed(field.flag) {
			continue
		}
		value, _ := cmd.Flags().GetString(field.flag)
		value = strings.TrimSpace(value)
		if value == "" {
			delete(values, field.key)
			continue
		}
		encoded, _ := json.Marshal(value)
		values[field.key] = encoded
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if _, err := types.ParseRecurrence(encoded, assignee); err != nil {
		return nil, err
	}
	return encoded, nil
}

func recurrenceMetadataEdits(cmd *cobra.Command) (set, unset []string, err error) {
	clearAll := cmd.Flags().Changed("repeat")
	if clearAll {
		repeat, _ := cmd.Flags().GetString("repeat")
		clearAll = strings.TrimSpace(repeat) == ""
	}
	if clearAll {
		for _, field := range recurrenceFlags[1:] {
			if cmd.Flags().Changed(field.flag) {
				return nil, nil, fmt.Errorf("--repeat='' clears the whole recurrence contract and cannot be combined with --%s", field.flag)
			}
		}
		for _, field := range recurrenceFlags {
			unset = append(unset, field.key)
		}
		return nil, unset, nil
	}
	for _, field := range recurrenceFlags {
		if !cmd.Flags().Changed(field.flag) {
			continue
		}
		value, _ := cmd.Flags().GetString(field.flag)
		value = strings.TrimSpace(value)
		if value == "" {
			unset = append(unset, field.key)
		} else {
			set = append(set, field.key+"="+value)
		}
	}
	return set, unset, nil
}
