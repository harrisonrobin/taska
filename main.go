package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/harrisonrobin/taska/pkg/auth"
	"github.com/harrisonrobin/taska/pkg/config"
	"github.com/harrisonrobin/taska/pkg/google"
	"github.com/harrisonrobin/taska/pkg/index"
	"github.com/harrisonrobin/taska/pkg/overdue"
	"github.com/harrisonrobin/taska/pkg/taskwarrior"
	"google.golang.org/api/calendar/v3"
)

func main() {
	// 1. Parse Flags
	calendarName := flag.String("calendar", "", "Google Calendar name to sync with (overrides config)")
	setCalendar := flag.String("set-calendar", "", "Set the default Google Calendar name")
	doAuth := flag.Bool("auth", false, "Authenticate with Google Calendar")
	background := flag.Bool("background", false, "Internal use: run in background mode")
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

	// 5. Handle Foreground vs Background Mode
	client := taskwarrior.NewClient()

	if !*background {
		// FOREGROUND: Read tasks, print to stdout, spawn background, exit.
		twTasks, err := client.ParseTasks(os.Stdin)
		if err != nil {
			log.Fatalf("Error parsing tasks from stdin: %v", err)
		}

		// Protocol: Output result JSON
		if len(twTasks) > 0 {
			taskToOutput := twTasks[len(twTasks)-1]
			if err := json.NewEncoder(os.Stdout).Encode(taskToOutput); err != nil {
				log.Printf("Error encoding task to stdout: %v", err)
			}
		}

		if len(twTasks) == 0 {
			return
		}

		// Spawn background process
		self, err := os.Executable()
		if err != nil {
			log.Fatalf("could not find self: %v", err)
		}
		args := []string{"--background", "--calendar", selectedCalendar}
		cmd := exec.Command(self, args...)
		cmd.Stdout = nil // Silence in background
		cmd.Stderr = nil // Silence in background

		// Encode tasks to pass via pipe
		stdin, err := cmd.StdinPipe()
		if err != nil {
			log.Fatalf("could not open stdin pipe: %v", err)
		}
		if err := cmd.Start(); err != nil {
			log.Fatalf("could not start background process: %v", err)
		}

		json.NewEncoder(stdin).Encode(twTasks)
		stdin.Close()

		// Detach and exit
		return
	}

	// BACKGROUND: Performance heavy lifting
	twTasks, err := client.ParseTasks(os.Stdin)
	if err != nil {
		log.Fatalf("Background: error parsing tasks: %v", err)
	}

	sweepTable, err := overdue.NewTable()
	if err != nil {
		log.Printf("Warning: failed to initialize overdue sweep table: %v", err)
	}

	evtIndex, err := index.NewEventIndex()
	if err != nil {
		log.Printf("Warning: failed to initialize event index: %v", err)
	}

	gClient, err := google.NewClient(selectedCalendar, evtIndex)
	if err != nil {
		log.Printf("Error creating Google Calendar client: %v", err)
		return
	}

	// Run Overdue Sweep
	if sweepTable != nil {
		overdueEntries := sweepTable.Sweep(time.Now())
		for _, e := range overdueEntries {
			patch := &calendar.Event{
				Summary: "! " + e.Summary,
			}
			if _, err := gClient.PatchEvent(e.GCalID, patch); err != nil {
				log.Printf("Sweep: error patching event %s: %v", e.GCalID, err)
			}
		}
		// Save table after sweep
		if err := sweepTable.Save(); err != nil {
			log.Printf("Warning: failed to save sweep table: %v", err)
		}
	}

	// Process Hook Tasks
	if len(twTasks) == 0 {
		return
	}

	var taskToSync *taskwarrior.Task
	action := "sync" // default

	if len(twTasks) == 1 {
		taskToSync = &twTasks[0]
	} else if len(twTasks) >= 2 {
		newT := &twTasks[1]
		taskToSync = newT

		isBlockedOrWaiting := false
		if newT.Status == "waiting" {
			isBlockedOrWaiting = true
		}
		for _, tag := range newT.Tags {
			if tag == "BLOCKED" {
				isBlockedOrWaiting = true
				break
			}
		}
		if isBlockedOrWaiting || newT.Status == "deleted" {
			action = "delete"
		}
	}

	if taskToSync == nil {
		return
	}

	if action == "delete" {
		event, err := gClient.GetEventByTaskID(taskToSync.UUID)
		if err == nil && event != nil {
			if err := gClient.DeleteEvent(event.Id); err != nil {
				log.Printf("Error deleting event: %v", err)
			}
		}
		if sweepTable != nil {
			sweepTable.Remove(taskToSync.UUID)
			sweepTable.Save()
		}
		if evtIndex != nil {
			evtIndex.Remove(taskToSync.UUID)
			evtIndex.Save()
		}
	} else {
		event, err := gClient.SyncEvent(*taskToSync)
		if err != nil {
			log.Printf("Error syncing event: %v\n", err)
		} else if sweepTable != nil && taskToSync.Status == "pending" && taskToSync.Scheduled != nil {
			sweepTable.Update(taskToSync.UUID, event.Id, taskToSync.Description, taskToSync.Scheduled.Time)
			sweepTable.Save()
		}
		if evtIndex != nil {
			evtIndex.Save() // Save new mappings
		}
	}
}
