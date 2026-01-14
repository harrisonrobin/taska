package overdue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/harrisonrobin/taska/pkg/model"
)

type Entry struct {
	UUID      string    `json:"uuid"`
	Scheduled time.Time `json:"scheduled"`
}

type Table struct {
	Entries []Entry `json:"entries"`
	Path    string  `json:"-"`
}

func NewTable() (*Table, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "taska", "pending_tasks.json")

	t := &Table{
		Path:    path,
		Entries: []Entry{},
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
	return encoder.Encode(t)
}

// Update adds or updates a task in the table if it's pending and has a future scheduled date.
// Otherwise, it removes it.
func (t *Table) Update(task model.Task) {
	// Remove if it exists first
	t.Remove(task.ID)

	// Add back only if relevant (pending and has future/current scheduled date)
	// We check for !task.Scheduled.IsZero() and status == "pending"
	if task.Status == "pending" && !task.Scheduled.IsZero() {
		t.Entries = append(t.Entries, Entry{
			UUID:      task.ID,
			Scheduled: task.Scheduled,
		})
		sort.Slice(t.Entries, func(i, j int) bool {
			return t.Entries[i].Scheduled.Before(t.Entries[j].Scheduled)
		})
	}
}

func (t *Table) Remove(uuid string) {
	for i, e := range t.Entries {
		if e.UUID == uuid {
			t.Entries = append(t.Entries[:i], t.Entries[i+1:]...)
			return
		}
	}
}

// Sweep returns UUIDs of tasks that have become overdue (Scheduled < now) and removes them.
func (t *Table) Sweep(now time.Time) []string {
	var swept []string
	idx := 0
	for idx < len(t.Entries) && t.Entries[idx].Scheduled.Before(now) {
		swept = append(swept, t.Entries[idx].UUID)
		idx++
	}

	if idx > 0 {
		t.Entries = t.Entries[idx:]
	}

	return swept
}
