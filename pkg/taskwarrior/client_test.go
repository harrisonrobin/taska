package taskwarrior

import (
	"strings"
	"testing"
	"time"
)

func TestParseTask(t *testing.T) {
	input := `{
		"uuid": "f45a05b3-c12e-42e5-9c9c-333333333333",
		"description": "Buy milk",
		"status": "pending",
		"due": "20230101T120000Z",
		"project": "Groceries",
		"tags": ["buy", "food"],
		"annotations": [
			{"entry": "20230101T120500Z", "description": "Don't forget almond milk"}
		]
	}`

	client := NewClient()
	task, err := client.ParseTask(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseTask failed: %v", err)
	}

	if task.UUID != "f45a05b3-c12e-42e5-9c9c-333333333333" {
		t.Errorf("Expected UUID f45a05b3-c12e-42e5-9c9c-333333333333, got %s", task.UUID)
	}
	if task.Description != "Buy milk" {
		t.Errorf("Expected Description 'Buy milk', got '%s'", task.Description)
	}
	if task.Project != "Groceries" {
		t.Errorf("Expected Project 'Groceries', got '%s'", task.Project)
	}
	if len(task.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(task.Tags))
	}
	if len(task.Annotations) != 1 {
		t.Errorf("Expected 1 annotation, got %d", len(task.Annotations))
	}
	expectedDue, _ := time.Parse(time.RFC3339, "2023-01-01T12:00:00Z")
	if !task.Due.Time.Equal(expectedDue) {
		t.Errorf("Expected Due %v, got %v", expectedDue, task.Due.Time)
	}
}
