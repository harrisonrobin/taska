package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/harrisonrobin/taska/pkg/auth"
	"github.com/harrisonrobin/taska/pkg/config"
	"github.com/harrisonrobin/taska/pkg/google"
	"github.com/harrisonrobin/taska/pkg/model"
	"github.com/harrisonrobin/taska/pkg/overdue"
	"github.com/harrisonrobin/taska/pkg/taskwarrior"
)

// parseDuration parses ISO 8601 duration format (PT1H30M) from Taskwarrior JSON export
func parseDuration(s string) (time.Duration, error) {
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

func main() {
	// 1. Parse Flags
	calendarName := flag.String("calendar", "", "Google Calendar name to sync with (overrides config)")
	setCalendar := flag.String("set-calendar", "", "Set the default Google Calendar name")
	doAuth := flag.Bool("auth", false, "Authenticate with Google Calendar")
	flag.Parse()

	// 2. Handle Set Calendar
	if *setCalendar != "" {
		cfg := &config.Config{Calendar: *setCalendar}
		if err := config.Save(cfg); err != nil {
			log.Fatalf("Error saving config: %v", err)
		}
		fmt.Printf("Default calendar set to: %s\n", *setCalendar)
		return
	}

	// 3. Determine Calendar (Priority: Flag > Config > Default)
	selectedCalendar := "Tasks" // Default fallback
	cfg, err := config.Load()
	if err == nil && cfg.Calendar != "" {
		selectedCalendar = cfg.Calendar
	}
	if *calendarName != "" {
		selectedCalendar = *calendarName
	}

	// 4. Handle Authentication
	if *doAuth {
		ctx := context.Background()
		xdgConfigBase, err := auth.GetXdgHome()
		if err != nil {
			log.Fatalf("could not find path to configuration file: error %v", err)
		}

		tokenFile := filepath.Join(xdgConfigBase, auth.TokenFile)
		_, err = os.Stat(tokenFile)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("could not check token file '%s', error %v", tokenFile, err)
			}
		} else {
			log.Printf("Removing existing token file at '%s'\n", tokenFile)
			if err = os.Remove(tokenFile); err != nil {
				log.Fatalf("could not delete token file '%s', error %v. Please delete it manually", tokenFile, err)
			}
		}

		_, err = auth.GetCalendarService(ctx)
		if err != nil {
			log.Fatalf("Authentication failed: %v", err)
		}
		log.Printf("Authentication successful! Token saved to %s", auth.TokenFile)
		return
	}

	// 5. Initialize Overdue Sweep Table
	sweepTable, err := overdue.NewTable()
	if err != nil {
		log.Printf("Warning: failed to initialize overdue sweep table: %v", err)
	}

	// 6. Handle Hook Logic (Stdin)
	client := taskwarrior.NewClient()
	twTasks, err := client.ParseTasks(os.Stdin)
	if err != nil {
		log.Fatalf("Error parsing tasks from stdin: %v", err)
	}

	// PROTOCOL: We MUST output the input tasks back to stdout at the end.
	defer func() {
		if len(twTasks) > 0 {
			taskToOutput := twTasks[len(twTasks)-1]
			if err := json.NewEncoder(os.Stdout).Encode(taskToOutput); err != nil {
				log.Printf("Error encoding task to stdout: %v", err)
			}
		}

		if sweepTable != nil {
			if err := sweepTable.Save(); err != nil {
				log.Printf("Warning: failed to save sweep table: %v", err)
			}
		}
	}()

	// 7. Initialize Google Calendar Client
	gClient, err := google.NewClient(selectedCalendar)
	if err != nil {
		log.Printf("Error creating Google Calendar client: %v", err)
		return
	}

	// Helper to convert TW task to Model task
	toModel := func(twT taskwarrior.Task) *model.Task {
		var deadline time.Time
		if twT.Due != nil {
			deadline = twT.Due.Time
		}
		var scheduled time.Time
		if twT.Scheduled != nil {
			scheduled = twT.Scheduled.Time
		}
		var start, end time.Time
		if twT.Start != nil {
			start = twT.Start.Time
		}
		if twT.End != nil {
			end = twT.End.Time
		}
		est, _ := parseDuration(twT.Est)
		act, _ := parseDuration(twT.Act)

		t := &model.Task{
			ID:          twT.UUID,
			Description: twT.Description,
			Deadline:    deadline,
			Scheduled:   scheduled,
			Status:      twT.Status,
			Source:      "taskwarrior",
			Project:     twT.Project,
			Tags:        twT.Tags,
			Start:       start,
			End:         end,
			Estimate:    est,
			Actual:      act,
		}

		if len(twT.Annotations) > 0 {
			for _, a := range twT.Annotations {
				t.Annotations = append(t.Annotations, a.Description)
			}
		}
		return t
	}

	// 8. Run Overdue Sweep
	if sweepTable != nil {
		sweptUUIDs := sweepTable.Sweep(time.Now())
		for _, uuid := range sweptUUIDs {
			tasks, err := client.GetTasks([]string{uuid})
			if err != nil || len(tasks) == 0 {
				// It was already removed from the memory table by Sweep(),
				// and it will be saved to disk by the deferred save.
				continue
			}
			mt := toModel(tasks[0])
			if _, err := gClient.SyncEvent(*mt); err != nil {
				log.Printf("Sweep: error syncing task %s: %v", uuid, err)
			}
		}
	}

	if len(twTasks) == 0 {
		return
	}

	// 9. Process Hook Tasks
	var taskToSync *model.Task
	action := "sync" // default

	if len(twTasks) == 1 {
		// on-add (or manual single pipe)
		newTask := twTasks[0]
		taskToSync = toModel(newTask)

	} else if len(twTasks) >= 2 {
		// on-modify: [0]=old, [1]=new
		newT := twTasks[1]
		taskToSync = toModel(newT)

		isBlockedOrWaiting := false
		if newT.Status == "waiting" {
			isBlockedOrWaiting = true
		}
		// Check for BLOCKED tag
		for _, tag := range newT.Tags {
			if tag == "BLOCKED" {
				isBlockedOrWaiting = true
				break
			}
		}

		if isBlockedOrWaiting {
			action = "delete"
		} else if newT.Status == "deleted" {
			action = "delete"
		}
	}

	if taskToSync == nil {
		return
	}

	if action == "delete" {
		// Find and delete
		event, err := gClient.GetEventByTaskID(taskToSync.ID)
		if err != nil {
			log.Printf("Error finding event to delete: %v", err)
			return
		}
		if event != nil {
			err := gClient.DeleteEvent(event.Id)
			if err != nil {
				log.Printf("Error deleting event: %v", err)
			}
		}
		if sweepTable != nil {
			sweepTable.Remove(taskToSync.ID)
		}
	} else {
		// Insert / Patch
		_, err := gClient.SyncEvent(*taskToSync)
		if err != nil {
			log.Printf("Error syncing event for task %s: %v\n", taskToSync.Description, err)
		} else if sweepTable != nil {
			sweepTable.Update(*taskToSync)
		}
	}
}
