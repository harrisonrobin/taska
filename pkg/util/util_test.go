package util

import (
	"strings"
	"testing"
	"time"

	"github.com/harrisonrobin/taska/pkg/taskwarrior"
)

func TestConvertTaskToCalendarEvent(t *testing.T) {
	deadline := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	task := &taskwarrior.Task{
		UUID:        "12345678-1234-1234-1234-123456789012",
		Description: "Test Task",
		Status:      "pending",
		Due:         &taskwarrior.CustomTime{Time: deadline},
		Project:     "Work",
		Tags:        []string{"buy", "food"},
	}
	task.Annotations = append(task.Annotations, struct {
		Description string                  `json:"description"`
		Entry       *taskwarrior.CustomTime `json:"entry"`
	}{Description: "Note 1"})

	event, err := ConvertTaskToCalendarEvent(task)
	if err != nil {
		t.Fatalf("ConvertTaskToCalendarEvent failed: %v", err)
	}

	// Verify ExtendedProperties
	if event.ExtendedProperties == nil || event.ExtendedProperties.Private == nil {
		t.Fatal("ExtendedProperties or Private map is nil")
	}
	if val, ok := event.ExtendedProperties.Private["taskwarrior_id"]; !ok || val != task.UUID {
		t.Errorf("Expected taskwarrior_id %s, got %v", task.UUID, val)
	}

	// Verify Description contains Tags and Accounting
	if !strings.Contains(event.Description, "#buy #food") {
		// Tags might be separate
		if !strings.Contains(event.Description, "#buy") || !strings.Contains(event.Description, "#food") {
			t.Errorf("Expected description to contain tags, got: %s", event.Description)
		}
	}
	if !strings.Contains(event.Description, "Accounting:") {
		t.Errorf("Expected description to contain Accounting section, got: %s", event.Description)
	}

	// Verify Description contains Annotation
	if !strings.Contains(event.Description, "Note 1") {
		t.Errorf("Expected description to contain 'Note 1', got: %s", event.Description)
	}
}
