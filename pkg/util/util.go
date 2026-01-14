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

// EventNeedsUpdate returns true if the fields shared between a model.Task and a calendar.Event differ.
// It compares the target event (newly converted) with the existing event from the calendar.
func EventNeedsUpdate(task *model.Task, existingEvent *calendar.Event, targetEvent *calendar.Event) (bool, string, error) {
	// Status derived from Summary prefix
	var existingIsCompleted bool
	var existingIsDeleted bool
	var cleanSummary string

	if strings.HasPrefix(existingEvent.Summary, "✅") {
		existingIsCompleted = true
		cleanSummary = strings.TrimSpace(strings.TrimPrefix(existingEvent.Summary, "✅"))
	} else if strings.HasPrefix(existingEvent.Summary, "❌") {
		existingIsDeleted = true
		cleanSummary = strings.TrimSpace(strings.TrimPrefix(existingEvent.Summary, "❌"))
	} else {
		cleanSummary = existingEvent.Summary
	}

	// 1. Check for Status Mismatch
	if task.Status == "completed" && !existingIsCompleted {
		return true, NEEDS_UPDATE_STATUS, nil
	}
	if task.Status == "deleted" && !existingIsDeleted {
		return true, NEEDS_UPDATE_STATUS, nil
	}
	if task.Status == "pending" && (existingIsCompleted || existingIsDeleted) {
		return true, NEEDS_UPDATE_STATUS, nil
	}

	// 2. Check for Summary/Title Mismatch
	if task.Description != cleanSummary {
		log.Printf("Summary mismatch: task='%s', existing='%s'", task.Description, cleanSummary)
		return true, NEEDS_UPDATE_DESCRIPTION, nil
	}

	// 3. Check for Description (Annotations/Notes) Mismatch
	if existingEvent.Description != targetEvent.Description {
		log.Printf("Description mismatch (Annotations/Metadata) for task: %s", task.Description)
		return true, NEEDS_UPDATE_DESCRIPTION, nil
	}

	// 4. Check for Color Mismatch
	if existingEvent.ColorId != targetEvent.ColorId {
		log.Printf("Color mismatch for task: %s", task.Description)
		return true, "color", nil
	}

	// 5. Check for Time/Due Date Mismatch
	existingTime, err := time.Parse(time.RFC3339, existingEvent.Start.DateTime)
	if err != nil {
		return false, "", err
	}

	targetTime, err := time.Parse(time.RFC3339, targetEvent.Start.DateTime)
	if err != nil {
		return false, "", err
	}

	if !existingTime.Equal(targetTime) {
		log.Printf("Time mismatch: existing=%s, target=%s", existingTime, targetTime)
		return true, NEEDS_UPDATE_DUE, nil
	}

	return false, "", nil
}

func ConvertTaskToCalendarEvent(task *model.Task) (*calendar.Event, error) {
	if task == nil {
		return nil, fmt.Errorf("could not convert nil Task")
	}

	// 1. Title/Summary Logic
	prefix := ""
	now := time.Now()

	if task.Status == "completed" {
		prefix = "✓"
	} else if !task.Start.IsZero() {
		// Started / Active
		prefix = "‣"
	} else if (!task.Deadline.IsZero() && task.Deadline.Before(now)) || (!task.Scheduled.IsZero() && task.Scheduled.Before(now)) {
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
	} else if !task.Scheduled.IsZero() {
		// Scheduled
		start = task.Scheduled
		if task.Estimate > 0 {
			end = start.Add(task.Estimate)
		} else {
			end = start.Add(defaultDuration)
		}
	} else if !task.Deadline.IsZero() {
		// Due
		start = task.Deadline
		if task.Estimate > 0 {
			end = start.Add(task.Estimate)
		} else {
			end = start.Add(defaultDuration)
		}
	} else {
		// ROI: If no dates, we can't sync it easily.
		return nil, fmt.Errorf("task has no date usage (due, start, scheduled, or end): %s", task.ID)
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
	descBuilder.WriteString(fmt.Sprintf("UUID: %s\n", task.ID))

	// Accounting Bullets
	descBuilder.WriteString("\nAccounting:\n")
	if task.Estimate > 0 {
		descBuilder.WriteString(fmt.Sprintf("• estimated: %s\n", task.Estimate))
	}

	// Started late/early calculation
	if !task.Start.IsZero() && !task.Scheduled.IsZero() {
		diff := task.Start.Sub(task.Scheduled)
		if diff > time.Minute {
			descBuilder.WriteString(fmt.Sprintf("• started late by: %s\n", diff.Round(time.Minute)))
		} else if diff < -time.Minute {
			descBuilder.WriteString(fmt.Sprintf("• started early by: %s\n", (-diff).Round(time.Minute)))
		}
	}

	if task.Status == "completed" {
		// Calculate actual time spent
		var spent time.Duration
		if task.Actual > 0 {
			// Use UDA 'act' field if available
			spent = task.Actual
		} else if !task.Start.IsZero() && !task.End.IsZero() {
			// Calculate from start/end timestamps
			spent = task.End.Sub(task.Start)
		}

		if spent > 0 {
			descBuilder.WriteString(fmt.Sprintf("• spent: %s\n", spent))
			if task.Estimate > 0 {
				diff := spent - task.Estimate
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
