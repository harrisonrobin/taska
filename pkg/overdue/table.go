package overdue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	GCalID    string    `json:"gcal_id"`
	Summary   string    `json:"summary"`
	Scheduled time.Time `json:"scheduled"`
}

type Table struct {
	Entries map[string]Entry `json:"entries"`
	Path    string           `json:"-"`
	dirty   bool
}

func NewTable() (*Table, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "taska", "pending_tasks.json")

	t := &Table{
		Path:    path,
		Entries: make(map[string]Entry),
	}

	if _, err := os.Stat(path); err == nil {
		if err := t.Load(); err != nil {
			return nil, err
		}
	}

	return t, nil
}

func (t *Table) Load() error {
	f, err := os.Open(t.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(t)
}

func (t *Table) Save() error {
	if !t.dirty {
		return nil
	}
	dir := filepath.Dir(t.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.Create(t.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(t)
	if err == nil {
		t.dirty = false
	}
	return err
}

// Update adds or updates a task in the table if it's pending and has a future scheduled date.
// Otherwise, it removes it.
func (t *Table) Update(uuid string, gcalID string, summary string, scheduled time.Time) {
	if !scheduled.IsZero() {
		old, exists := t.Entries[uuid]
		if !exists || !old.Scheduled.Equal(scheduled) || old.GCalID != gcalID || old.Summary != summary {
			t.Entries[uuid] = Entry{
				GCalID:    gcalID,
				Summary:   summary,
				Scheduled: scheduled,
			}
			t.dirty = true
		}
	} else {
		t.Remove(uuid)
	}
}

func (t *Table) Remove(uuid string) {
	if _, exists := t.Entries[uuid]; exists {
		delete(t.Entries, uuid)
		t.dirty = true
	}
}

// Sweep returns entries that have become overdue (Scheduled < now) and removes them.
func (t *Table) Sweep(now time.Time) []Entry {
	var swept []Entry
	for uuid, entry := range t.Entries {
		if entry.Scheduled.Before(now) {
			swept = append(swept, entry)
			delete(t.Entries, uuid)
			t.dirty = true
		}
	}
	return swept
}
