package google

import (
	"fmt"
	"log"
	"time"

	"github.com/harrisonrobin/taska/pkg/index"
	"github.com/harrisonrobin/taska/pkg/taskwarrior"
	"github.com/harrisonrobin/taska/pkg/util"
	"google.golang.org/api/calendar/v3"
)

// CalendarClient is a Google Calendar API client.
type CalendarClient struct {
	srv        *calendar.Service
	calendarID string
	index      *index.EventIndex
}

// NewCalendarClient creates a new Google Calendar client.
func NewCalendarClient(srv *calendar.Service, calendarID string, idx *index.EventIndex) *CalendarClient {
	return &CalendarClient{srv: srv, calendarID: calendarID, index: idx}
}

// SyncEvent creates a new event or updates an existing one.
func (c *CalendarClient) SyncEvent(task taskwarrior.Task) (*calendar.Event, error) {
	event, err := util.ConvertTaskToCalendarEvent(&task)
	if err != nil {
		return nil, err
	}

	var existingEvent *calendar.Event
	// 1. Try local index first
	if c.index != nil {
		eventID := c.index.Get(task.UUID)
		if eventID != "" {
			existingEvent, err = c.srv.Events.Get(c.calendarID, eventID).Do()
			if err != nil {
				// If not found or error, fallback to search
				existingEvent = nil
			}
		}
	}

	// 2. Fallback to API search if not found in index or index failed
	if existingEvent == nil {
		existingEvent, err = c.GetEventByTaskID(task.UUID)
		if err != nil {
			return nil, fmt.Errorf("error searching for event: %w", err)
		}
	}

	if existingEvent != nil {
		patch, err := util.EventNeedsUpdate(&task, existingEvent, event)
		if err != nil {
			log.Printf("could not compare task with its calendar event: %v", err)
			return nil, err
		}
		if patch != nil {
			// Surgical Patch
			updatedEvent, err := c.PatchEvent(existingEvent.Id, patch)
			if err == nil && c.index != nil {
				c.index.Set(task.UUID, updatedEvent.Id)
			}
			return updatedEvent, err
		}
		return existingEvent, nil
	}

	createdEvent, err := c.srv.Events.Insert(c.calendarID, event).Do()
	if err == nil && c.index != nil {
		c.index.Set(task.UUID, createdEvent.Id)
	}
	return createdEvent, err
}

// PatchEvent performs a partial update on an event.
func (c *CalendarClient) PatchEvent(eventID string, patch *calendar.Event) (*calendar.Event, error) {
	return c.srv.Events.Patch(c.calendarID, eventID, patch).Do()
}

// DeleteEvent deletes an event from the calendar.
func (c *CalendarClient) DeleteEvent(eventID string) error {
	return c.srv.Events.Delete(c.calendarID, eventID).Do()
}

// ListEvents fetches events from the calendar within a given time range.
func (c *CalendarClient) ListEvents(timeMin time.Time) ([]*calendar.Event, error) {
	events, err := c.srv.Events.List(c.calendarID).TimeMin(timeMin.Format(time.RFC3339)).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve events from calendar: %w", err)
	}
	return events.Items, nil
}

// GetEventByTaskID searches for an event with the given Taskwarrior ID in extended properties.
func (c *CalendarClient) GetEventByTaskID(taskID string) (*calendar.Event, error) {
	// Look for private extended property 'taskwarrior_id'
	events, err := c.srv.Events.List(c.calendarID).
		PrivateExtendedProperty(fmt.Sprintf("taskwarrior_id=%s", taskID)).
		Do()
	if err != nil {
		return nil, err
	}
	if len(events.Items) > 0 {
		return events.Items[0], nil
	}
	return nil, nil
}
