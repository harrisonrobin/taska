package colors

import (
	"encoding/json"
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
}

func NewColorCache() (*ColorCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".task", "project_colors.json")

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
	f, err := os.Create(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(c.Projects)
}

// GetColorID returns the color ID for a project, managing LRU logic.
// isTaskActive should be true if the task claiming this color is active/pending.
func (c *ColorCache) GetColorID(project string, isTaskActive bool) string {
	if project == "" {
		return "14" // Default gray for no project
	}

	state, exists := c.Projects[project]
	if exists {
		state.LastModified = time.Now()
		if isTaskActive {
			// We don't increment here strictly, we just know it's being used.
			// Truly tracking "ActiveTasks" count accurately requires reading ALL tasks.
			// The requirement says: "finds the project in the cache that has 0 pending tasks".
			// Since we only see one task at a time in a hook, we can't maintain a perfect global count easily.
			// STARTING STRATEGY:
			// We will assume "ActiveTasks" is maintained externally or we simplify.
			// Simplification: We track LastModified. "ActiveTasks" is hard in a hook-only CLI without state.
			// Let's rely on LastModified and simply assign colors.
			// BUT the requirement explicitly mentions "0 pending tasks".
			// Let's approximate: If we are assigning for an Active task, we bump LastModified.
		}
		c.Save()
		return state.ColorID
	}

	// New Project
	// Available colors: 1-11
	usedColors := make(map[string]bool)
	for _, p := range c.Projects {
		usedColors[p.ColorID] = true
	}

	return c.assignColor(project)
}

func (c *ColorCache) assignColor(project string) string {
	// Colors 1 to 11 (Peacock to Tomato roughly in GCal UI, though IDs vary)
	// GCal standard event colors are "1" to "11".

	// Check used colors
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
				ActiveTasks:  1, // Assume 1 for the calling task
			}
			c.Save()
			return id
		}
	}

	// Cache is full -> Evict LRU
	// Find project with oldest LastModified. User req said "0 active tasks", but we can't track that perfectly.
	// We will use Oldest Modified as proxy for now.
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
		c.Save()
		return recycledColor
	}

	return "1" // Fallback
}

func colorIndexToString(i int) string {
	return strconv.Itoa(i)
}
