package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type EventIndex struct {
	Mappings map[string]string `json:"mappings"`
	Path     string            `json:"-"`
	mu       sync.RWMutex
	dirty    bool
}

func NewEventIndex() (*EventIndex, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "taska", "events.json")

	idx := &EventIndex{
		Mappings: make(map[string]string),
		Path:     path,
	}

	if _, err := os.Stat(path); err == nil {
		if err := idx.Load(); err != nil {
			return nil, err
		}
	}

	return idx, nil
}

func (idx *EventIndex) Load() error {
	f, err := os.Open(idx.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&idx.Mappings)
}

func (idx *EventIndex) Save() error {
	idx.mu.RLock()
	if !idx.dirty {
		idx.mu.RUnlock()
		return nil
	}
	idx.mu.RUnlock()

	idx.mu.Lock()
	defer idx.mu.Unlock()

	dir := filepath.Dir(idx.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.Create(idx.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(idx.Mappings); err != nil {
		return err
	}
	idx.dirty = false
	return nil
}

func (idx *EventIndex) Get(taskID string) string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.Mappings[taskID]
}

func (idx *EventIndex) Set(taskID, eventID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.Mappings[taskID] != eventID {
		idx.Mappings[taskID] = eventID
		idx.dirty = true
	}
}

func (idx *EventIndex) Remove(taskID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, exists := idx.Mappings[taskID]; exists {
		delete(idx.Mappings, taskID)
		idx.dirty = true
	}
}
