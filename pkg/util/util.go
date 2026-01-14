package util

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/harrisonrobin/taska/pkg/colors"
	"github.com/harrisonrobin/taska/pkg/model"
	"google.golang.org/api/calendar/v3"
)

const (
	NEEDS_UPDATE_DESCRIPTION = "description"
	NEEDS_UPDATE_STATUS      = "status"
	NEEDS_UPDATE_DUE         = "due"
)

// EventNeedsUpdate returns true if the fields shared between a model.Task and a calendar.Event differ
func EventNeedsUpdate(task *model.Task, event *calendar.Event) (bool, string, error) {
	var eventIsCompleted bool
	var eventIsDeleted bool
	var cleanSummary string

	if strings.HasPrefix(event.Summary, "✅") {
		eventIsCompleted = true
		cleanSummary = strings.TrimSpace(strings.TrimPrefix(event.Summary, "✅"))
	} else if strings.HasPrefix(event.Summary, "❌") {
		eventIsDeleted = true
		cleanSummary = strings.TrimSpace(strings.TrimPrefix(event.Summary, "❌"))
	} else {
		cleanSummary = event.Summary
	}

	// Check for status mismatches
	if task.Status == "completed" && !eventIsCompleted {
		return true, NEEDS_UPDATE_STATUS, nil
	}
	if task.Status == "deleted" && !eventIsDeleted {
		return true, NEEDS_UPDATE_STATUS, nil
	}
	if task.Status == "pending" && (eventIsCompleted || eventIsDeleted) {
		return true, NEEDS_UPDATE_STATUS, nil
	}

	// Check for description mismatch
	if task.Description != cleanSummary {
		log.Printf("task: '%s', event: '%s' needs update\n", task.Description, cleanSummary)
		return true, NEEDS_UPDATE_DESCRIPTION, nil
	}

	// Check for due date mismatch
	eventTime, err := time.Parse(time.RFC3339, event.Start.DateTime)
	if err != nil {
		return false, "", err
	}

	if !eventTime.Equal(task.Deadline) {
		log.Printf("task: %s, event: %s time needs update\n", task.Description, event.Summary)
		return true, NEEDS_UPDATE_DUE, nil
	}

	return false, "", nil
}

func ConvertTaskToCalendarEvent(task *model.Task) (*calendar.Event, error) {
	if task == nil {
		return nil, fmt.Errorf("could not convert nil Task")
	}

	// 1. Title/Summary Logic
	prefix := "‣"
	if task.Status == "completed" {
		prefix = "✓"
	}
	// TODO: Check overdue for "!" prefix
	eventSummary := fmt.Sprintf("%s %s", prefix, task.Description)

	// 2. Color Logic
	// We need to instantiate ColorCache. Since this function is pure utility, we might need to pass cache or instantiate it here.
	// Instantiating here for now as user requested refactor. Best practice would be dependency injection but let's keep it simple.
	colorID := "14" // Default gray
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
	// Fallback: Start = Due, End = Due + Duration

	if task.Status == "completed" {
		if !task.End.IsZero() {
			end = task.End
		} else {
			end = time.Now()
		}

		duration := defaultDuration
		if task.Actual > 0 {
			duration = task.Actual
		} else if task.Estimate > 0 {
			duration = task.Estimate
		}
		start = end.Add(-duration)
	} else if !task.Start.IsZero() {
		// Started
		start = task.Start
		if task.Estimate > 0 {
			end = start.Add(task.Estimate)
		} else {
			end = start.Add(defaultDuration)
		}
	} else if !task.Deadline.IsZero() {
		// Scheduled / Due
		start = task.Deadline
		if task.Estimate > 0 {
			end = start.Add(task.Estimate)
		} else {
			end = start.Add(defaultDuration)
		}
	} else {
		// ROI: If no dates, we can't sync it easily.
		// User req said: "If no Start/End, ignore?"
		// actually user said "task add Check if +READY". Ready usually implies sched/due or just unblocked.
		// If no date, GCal requires one. Let's error or skip?
		// Legacy behavior was error.
		return nil, fmt.Errorf("task has no date usage (due, start, or end): %s", task.ID)
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
	descBuilder.WriteString(fmt.Sprintf("Project: %s\n", task.Project))
	descBuilder.WriteString(fmt.Sprintf("UUID: %s\n", task.ID))

	// Accounting Bullets
	descBuilder.WriteString("\nAccounting:\n")
	if task.Estimate > 0 {
		descBuilder.WriteString(fmt.Sprintf("• estimated: %s\n", task.Estimate))
	}
	if task.Status == "completed" {
		// Simple logic: if actual vs estimate logic exists.
		descBuilder.WriteString("• spent [calculation pending implementation]\n")
		// TODO: Refine with actual calculation if Act is present
	} else if !task.Start.IsZero() {
		// "started late/early" logic requires Scheduled date vs Start date
		descBuilder.WriteString("• started [check implementation]\n")
	}

	// Annotations
	if len(task.Annotations) > 0 {
		descBuilder.WriteString("\nNotes:\n")
		for _, ann := range task.Annotations {
			descBuilder.WriteString(fmt.Sprintf("‣ %s\n", ann))
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
				"taskwarrior_id": task.ID,
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
