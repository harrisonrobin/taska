/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/harrisonrobin/taska/pkg/auth"
	"github.com/harrisonrobin/taska/pkg/google"
	"github.com/harrisonrobin/taska/pkg/model"
	"github.com/harrisonrobin/taska/pkg/taskwarrior"
)

func main() {
	// 1. Parse Flags
	calendarName := flag.String("calendar", "Tasks", "Google Calendar name to sync with")
	doAuth := flag.Bool("auth", false, "Authenticate with Google Calendar")
	flag.Parse()

	// 2. Handle Authentication
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

	// 3. Handle Hook Logic (Stdin)
	// Hook input: 1 line (on-add) or 2 lines (on-modify: old, new)
	client := taskwarrior.NewClient()
	twTasks, err := client.ParseTasks(os.Stdin)
	if err != nil {
		// If stdin is empty or not JSON, this might happen.
		// For a hook, we expect input. If run manually without input, this will error.
		// We'll log fatal.
		log.Fatalf("Error parsing tasks from stdin: %v", err)
	}

	if len(twTasks) == 0 {
		return
	}

	var taskToSync *model.Task
	action := "sync" // default

	// Helper to convert TW task to Model task
	toModel := func(twT taskwarrior.Task) *model.Task {
		var deadline time.Time
		if twT.Due != nil {
			deadline = twT.Due.Time
		}
		var start, end time.Time
		if twT.Start != nil {
			start = twT.Start.Time
		}
		if twT.End != nil {
			end = twT.End.Time
		}
		est, _ := time.ParseDuration(twT.Est)
		act, _ := time.ParseDuration(twT.Act)

		t := &model.Task{
			ID:          twT.UUID,
			Description: twT.Description,
			Deadline:    deadline,
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

	// LOGIC MATRIX
	if len(twTasks) == 1 {
		// on-add (or manual single pipe)
		newTask := twTasks[0]
		taskToSync = toModel(newTask)

	} else if len(twTasks) >= 2 {
		// on-modify: [0]=old, [1]=new
		// oldT := twTasks[0]
		newT := twTasks[1]
		taskToSync = toModel(newT)

		isBlockedOrWaiting := false
		if newT.Status == "waiting" {
			isBlockedOrWaiting = true
		}
		// TODO: Add logic for +BLOCKED if accessible via tags or other means

		if isBlockedOrWaiting {
			action = "delete"
		} else if newT.Status == "deleted" {
			action = "delete"
		}

		// Additional logic for Start/Done transitions is handled by syncing the new state,
		// relying on the Calendar sync logic to update time/color/prefix.
	}

	if taskToSync == nil {
		return
	}

	// Perform Action
	gClient, err := google.NewClient(*calendarName)
	if err != nil {
		log.Fatalf("Error creating Google Calendar client: %v", err)
	}

	if action == "delete" {
		// Find and delete
		event, err := gClient.GetEventByTaskID(taskToSync.ID)
		if err != nil {
			log.Printf("Error finding event to delete: %v", err)
			return
		}
		if event != nil {
			log.Printf("Deleting event for task %s", taskToSync.ID)
			err := gClient.DeleteEvent(event.Id)
			if err != nil {
				log.Printf("Error deleting event: %v", err)
			}
		}
	} else {
		// Insert / Patch
		fmt.Printf("Syncing task: '%s', uuid: %s, due: %s\n", taskToSync.Description, taskToSync.ID, taskToSync.Deadline)
		_, err := gClient.SyncEvent(*taskToSync)
		if err != nil {
			fmt.Printf("Error syncing event for task %s: %v\n", taskToSync.Description, err)
		}
	}
}
