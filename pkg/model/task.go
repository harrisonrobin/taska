package model

import "time"

// Task represents a generic task from any source.
type Task struct {
	ID          string
	Description string
	Deadline    time.Time
	Tags        []string
	Priority    string
	Status      string
	Source      string // "taskwarrior" or "orgmode"
	Project     string
	Annotations []string
	// Accounting & Time-Shift
	Estimate time.Duration
	Actual   time.Duration
	Start    time.Time
	End      time.Time
	// Raw UDA maps if needed, or mapped fields
	UDA map[string]interface{}
}
