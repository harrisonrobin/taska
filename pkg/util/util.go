package util

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/harrisonrobin/taska/pkg/colors"
	"github.com/harrisonrobin/taska/pkg/taskwarrior"
	"google.golang.org/api/calendar/v3"
)

const (
	NEEDS_UPDATE_DESCRIPTION = "description"
	NEEDS_UPDATE_STATUS      = "status"
	NEEDS_UPDATE_DUE         = "due"
)

// ParseDuration parses ISO 8601 duration format (PT1H30M) from Taskwarrior JSON export
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	// Parse ISO 8601 format (PT1H, PT30M, PT1H30M)
	if len(s) < 2 || s[0] != 'P' {
		return 0, fmt.Errorf("invalid ISO 8601 duration format: %s", s)
	}

	// Remove 'P' prefix and check for 'T' (time component)
	s = s[1:]
	if len(s) == 0 || s[0] != 'T' {
		return 0, fmt.Errorf("invalid ISO 8601 duration (missing T): P%s", s)
	}
	s = s[1:] // Remove 'T'

	var total time.Duration
	re := regexp.MustCompile(`(\d+)([HMS])`)
	matches := re.FindAllStringSubmatch(s, -1)

	for _, match := range matches {
		value, _ := strconv.Atoi(match[1])
		unit := match[2]

		switch unit {
		case "H":
			total += time.Duration(value) * time.Hour
		case "M":
			total += time.Duration(value) * time.Minute
		case "S":
			total += time.Duration(value) * time.Second
		}
	}

	if total == 0 {
		return 0, fmt.Errorf("invalid ISO 8601 duration: PT%s", s)
	}

	return total, nil
}

// EventNeedsUpdate returns a patch event if the fields shared between a taskwarrior.Task and a calendar.Event differ.
// It compares the target event (newly converted) with the existing event from the calendar.
func EventNeedsUpdate(task *taskwarrior.Task, existingEvent *calendar.Event, targetEvent *calendar.Event) (*calendar.Event, error) {
	patch := &calendar.Event{}
	needsUpdate := false

	// 1. Check for Summary/Title Mismatch
	if existingEvent.Summary != targetEvent.Summary {
		patch.Summary = targetEvent.Summary
		needsUpdate = true
	}

	// 2. Check for Description (Annotations/Notes) Mismatch
	if existingEvent.Description != targetEvent.Description {
		patch.Description = targetEvent.Description
		needsUpdate = true
	}

	// 3. Check for Color Mismatch
	if existingEvent.ColorId != targetEvent.ColorId {
		patch.ColorId = targetEvent.ColorId
		needsUpdate = true
	}

	// 4. Check for Time/Due Date Mismatch
	existingStartTime, err := time.Parse(time.RFC3339, existingEvent.Start.DateTime)
	if err != nil {
		return nil, err
	}
	targetStartTime, err := time.Parse(time.RFC3339, targetEvent.Start.DateTime)
	if err != nil {
		return nil, err
	}
	existingEndTime, err := time.Parse(time.RFC3339, existingEvent.End.DateTime)
	if err != nil {
		return nil, err
	}
	targetEndTime, err := time.Parse(time.RFC3339, targetEvent.End.DateTime)
	if err != nil {
		return nil, err
	}

	if !existingStartTime.Equal(targetStartTime) || !existingEndTime.Equal(targetEndTime) {
		patch.Start = targetEvent.Start
		patch.End = targetEvent.End
		needsUpdate = true
	}

	if needsUpdate {
		return patch, nil
	}
	return nil, nil
}

func ConvertTaskToCalendarEvent(task *taskwarrior.Task) (*calendar.Event, error) {
	if task == nil {
		return nil, fmt.Errorf("could not convert nil Task")
	}

	est, _ := ParseDuration(task.Est)
	act, _ := ParseDuration(task.Act)

	// 1. Title/Summary Logic
	prefix := ""
	now := time.Now()

	if task.Status == "completed" {
		prefix = "✓"
	} else if task.Start != nil && !task.Start.IsZero() {
		// Started / Active
		prefix = "‣"
	} else if (task.Due != nil && !task.Due.IsZero() && task.Due.Before(now)) || (task.Scheduled != nil && !task.Scheduled.IsZero() && task.Scheduled.Before(now)) {
		// Overdue
		prefix = "!"
	}

	eventSummary := task.Description
	if prefix != "" {
		eventSummary = fmt.Sprintf("%s %s", prefix, task.Description)
	}

	// 2. Color Logic
	// We need to instantiate ColorCache. Since this function is pure utility, we might need to pass cache or instantiate it here.
	// Instantiating here for now as user requested refactor. Best practice would be dependency injection but let's keep it simple.
	colorID := "1" // Default lavender
	cache, err := colors.NewColorCache()
	if err == nil {
		isActive := task.Status == "pending" || task.Status == "waiting" // broad definition
		colorID = cache.GetColorID(task.Project, isActive)
	} else {
		log.Printf("Warning: could not load color cache: %v", err)
	}

	// 3. Time-Shift Logic
	var start, end time.Time

	// Default duration if nothing matches
	defaultDuration := 30 * time.Minute

	// Logic:
	// If Done: End = Now (or task.End), Start = End - Estimate/Actual/Duration
	// If Started: Start = Now (or task.Start), End = Start + Estimate
	// Fallback 1: Scheduled. Start = Scheduled, End = Scheduled + Estimate/Default
	// Fallback 2: Due. Start = Due, End = Due + Duration

	if task.Status == "completed" {
		if task.End != nil && !task.End.IsZero() {
			end = task.End.Time
		} else {
			end = time.Now()
		}

		duration := defaultDuration
		if act > 0 {
			duration = act
		} else if est > 0 {
			duration = est
		}
		start = end.Add(-duration)
	} else if task.Start != nil && !task.Start.IsZero() {
		// Started
		start = task.Start.Time
		if est > 0 {
			end = start.Add(est)
		} else {
			end = start.Add(defaultDuration)
		}
	} else if task.Scheduled != nil && !task.Scheduled.IsZero() {
		// Scheduled
		start = task.Scheduled.Time
		if est > 0 {
			end = start.Add(est)
		} else {
			end = start.Add(defaultDuration)
		}
	} else if task.Due != nil && !task.Due.IsZero() {
		// Due
		start = task.Due.Time
		if est > 0 {
			end = start.Add(est)
		} else {
			end = start.Add(defaultDuration)
		}
	} else {
		// ROI: If no dates, we can't sync it easily.
		return nil, fmt.Errorf("task has no date usage (due, start, scheduled, or end): %s", task.UUID)
	}

	// 4. Description & Accounting
	var descBuilder strings.Builder

	// Tags Header
	if len(task.Tags) > 0 {
		for _, tag := range task.Tags {
			descBuilder.WriteString(fmt.Sprintf("#%s ", tag))
		}
		descBuilder.WriteString("\n\n")
	}

	// Taskwarrior-y formatting
	descBuilder.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
	if task.Project != "" {
		descBuilder.WriteString(fmt.Sprintf("Project: %s\n", task.Project))
	}
	descBuilder.WriteString(fmt.Sprintf("UUID: %s\n", task.UUID))

	// Accounting Bullets
	descBuilder.WriteString("\nAccounting:\n")
	if est > 0 {
		descBuilder.WriteString(fmt.Sprintf("• estimated: %s\n", est))
	}

	// Started late/early calculation
	if task.Start != nil && !task.Start.IsZero() && task.Scheduled != nil && !task.Scheduled.IsZero() {
		diff := task.Start.Sub(task.Scheduled.Time)
		if diff > time.Minute {
			descBuilder.WriteString(fmt.Sprintf("• started late by: %s\n", diff.Round(time.Minute)))
		} else if diff < -time.Minute {
			descBuilder.WriteString(fmt.Sprintf("• started early by: %s\n", (-diff).Round(time.Minute)))
		}
	}

	if task.Status == "completed" {
		// Calculate actual time spent
		var spent time.Duration
		if act > 0 {
			// Use UDA 'act' field if available
			spent = act
		} else if task.Start != nil && !task.Start.IsZero() && task.End != nil && !task.End.IsZero() {
			// Calculate from start/end timestamps
			spent = task.End.Sub(task.Start.Time)
		}

		if spent > 0 {
			descBuilder.WriteString(fmt.Sprintf("• spent: %s\n", spent))
			if est > 0 {
				diff := spent - est
				if diff > 0 {
					descBuilder.WriteString(fmt.Sprintf("• over estimate by: %s\n", diff))
				} else if diff < 0 {
					descBuilder.WriteString(fmt.Sprintf("• under estimate by: %s\n", -diff))
				}
			}
		}
	}

	// Annotations
	if len(task.Annotations) > 0 {
		descBuilder.WriteString("\nNotes:\n")
		for _, ann := range task.Annotations {
			descBuilder.WriteString(fmt.Sprintf("‣ %s\n", ann.Description))
		}
	}

	event := &calendar.Event{
		Summary: eventSummary,
		ColorId: colorID,
		Start: &calendar.EventDateTime{
			DateTime: start.UTC().Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: end.UTC().Format(time.RFC3339),
		},
		Description: descBuilder.String(),
		ExtendedProperties: &calendar.EventExtendedProperties{
			Private: map[string]string{
				"taskwarrior_id": task.UUID,
			},
		},
	}

	return event, nil
}

// GetTaskIDFromEventDescription parses the task ID from the event description.
func GetTaskIDFromEventDescription(description string) (string, bool) {
	re := regexp.MustCompile(`ID: ([a-f0-9\-]+)`)
	matches := re.FindStringSubmatch(description)
	if len(matches) > 1 {
		return matches[1], true
	}
	return "", false
}
