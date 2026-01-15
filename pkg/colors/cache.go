package colors

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type ProjectState struct {
	ColorID      string    `json:"color_id"`
	ActiveTasks  int       `json:"active_tasks"`
	LastModified time.Time `json:"last_modified"`
}

type ColorCache struct {
	Path     string
	Projects map[string]*ProjectState `json:"projects"`
	dirty    bool
}

const (
	xdgAppName = "taska"
	cacheFile  = "project_colors.json"
)

func NewColorCache() (*ColorCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", xdgAppName, cacheFile)

	cache := &ColorCache{
		Path:     path,
		Projects: make(map[string]*ProjectState),
	}

	if _, err := os.Stat(path); err == nil {
		if err := cache.Load(); err != nil {
			return nil, err
		}
	}
	return cache, nil
}

func (c *ColorCache) Load() error {
	f, err := os.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(&c.Projects)
}

func (c *ColorCache) Save() error {
	if !c.dirty {
		return nil
	}
	// ensure directory exists
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Error creating color cache directory: %v", err)
		return err
	}

	f, err := os.Create(c.Path)
	if err != nil {
		log.Printf("Error creating color cache file: %v", err)
		return err
	}
	defer f.Close()
	err = json.NewEncoder(f).Encode(c.Projects)
	if err == nil {
		c.dirty = false
	}
	return err
}

// GetColorID returns the color ID for a project, managing LRU logic.
// isTaskActive should be true if the task claiming this color is active/pending.
func (c *ColorCache) GetColorID(project string, isTaskActive bool) string {
	if project == "" {
		return "14" // Default gray for no project
	}

	state, exists := c.Projects[project]
	if exists {
		// Just updating LastModified doesn't necessarily need a sync Save
		// unless we are VERY concerned about perfect LRU on crash.
		// For performance, we'll mark dirty but NOT call Save() here.
		state.LastModified = time.Now()
		c.dirty = true
		return state.ColorID
	}

	// New Project
	return c.assignColor(project)
}

func (c *ColorCache) assignColor(project string) string {
	// Colors 1 to 11 (Peacock to Tomato roughly)
	used := make(map[string]bool)
	for _, s := range c.Projects {
		used[s.ColorID] = true
	}

	// Try to find an unused slot
	for i := 1; i <= 11; i++ {
		id := colorIndexToString(i)
		if !used[id] {
			c.Projects[project] = &ProjectState{
				ColorID:      id,
				LastModified: time.Now(),
				ActiveTasks:  1,
			}
			c.dirty = true
			return id
		}
	}

	// Cache is full -> Evict LRU (Oldest Modified)
	var oldestProject string
	var oldestTime time.Time
	first := true

	for p, s := range c.Projects {
		if first || s.LastModified.Before(oldestTime) {
			oldestTime = s.LastModified
			oldestProject = p
			first = false
		}
	}

	if oldestProject != "" {
		recycledColor := c.Projects[oldestProject].ColorID
		delete(c.Projects, oldestProject)

		c.Projects[project] = &ProjectState{
			ColorID:      recycledColor,
			LastModified: time.Now(),
			ActiveTasks:  1,
		}
		c.dirty = true
		return recycledColor
	}

	return "1" // Fallback
}

func colorIndexToString(i int) string {
	return strconv.Itoa(i)
}
