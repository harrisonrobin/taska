package taskwarrior

import (
	"fmt"
	"strings"
	"time"
)

const (
	PENDING   = "pending"
	COMPLETED = "completed"
	WAITING   = "waiting"
	DELETED   = "deleted"
)

type CustomTime struct {
	time.Time
}

const taskwarriorTimeLayout = "20060102T150405Z" // YYYYMMDDTHHMMSSZ, 'Z' indicates UTC

// UnmarshalJSON implements the json.Unmarshaler interface for CustomTime.
func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`) // Remove surrounding quotes
	if s == "" || s == "0" {          // Handle empty string or "0" if Taskwarrior ever outputs it
		ct.Time = time.Time{} // Set to zero value
		return nil
	}

	t, err := time.Parse(taskwarriorTimeLayout, s)
	if err != nil {
		return fmt.Errorf("failed to parse Taskwarrior time string '%s': %w", s, err)
	}
	ct.Time = t
	return nil
}

// MarshalJSON implements the json.Marshaler interface for CustomTime.
func (ct CustomTime) MarshalJSON() ([]byte, error) {
	if ct.Time.IsZero() {
		return []byte(`""`), nil // Export zero time as empty string
	}
	return []byte(`"` + ct.Time.Format(taskwarriorTimeLayout) + `"`), nil
}

type Task struct {
	UUID        string      `json:"uuid"`
	Description string      `json:"description"`
	Due         *CustomTime `json:"due,omitempty"`
	Scheduled   *CustomTime `json:"scheduled,omitempty"`
	Status      string      `json:"status"`
	Project     string      `json:"project,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Annotations []struct {
		Description string      `json:"description"`
		Entry       *CustomTime `json:"entry"`
	} `json:"annotations,omitempty"`
	// Time Tracking & Accounting
	Start *CustomTime `json:"start,omitempty"`
	End   *CustomTime `json:"end,omitempty"`
	// UDA fields are often flat in JSON export, but let's check input.
	// Taskwarrior exports UDAs as top-level fields like "est" and "act" if configured.
	// We'll use a specific struct or map for them?
	// Using mapstructure or manual parsing is safer, but let's add specific known fields if user confirmed labels.
	// User said: uda.estimate.label=est, uda.actual.label=act
	Est string `json:"est,omitempty"` // Duration string like "1h"
	Act string `json:"act,omitempty"` // Duration string like "30m" -- Timewarrior format might differ?
	// Note: Timewarrior usually doesn't inject INTO the task JSON unless 'hook' does it or it's stored in UDA.
	// User implies it IS in UDA.
}
