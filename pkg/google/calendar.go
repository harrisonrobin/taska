package google

import (
	"fmt"
	"log"
	"time"

	"github.com/harrisonrobin/taska/pkg/model"
	"github.com/harrisonrobin/taska/pkg/util"
	"google.golang.org/api/calendar/v3"
)

// CalendarClient is a Google Calendar API client.
type CalendarClient struct {
	srv        *calendar.Service
	calendarID string
}

// NewCalendarClient creates a new Google Calendar client.
func NewCalendarClient(srv *calendar.Service, calendarID string) *CalendarClient {
	return &CalendarClient{srv: srv, calendarID: calendarID}
}

// SyncEvent creates a new event or updates an existing one.
func (c *CalendarClient) SyncEvent(task model.Task) (*calendar.Event, error) {
	event, err := util.ConvertTaskToCalendarEvent(&task)
	if err != nil {
		return nil, err
	}

	// Search for existing event by Extended Property
	existingEvent, err := c.GetEventByTaskID(task.ID)
	if err != nil {
		return nil, fmt.Errorf("error searching for event: %w", err)
	}

	if existingEvent != nil {
		needsUpdate, _, err := util.EventNeedsUpdate(&task, existingEvent, event)
		if err != nil {
			log.Printf("could not compare task with its calendar event: %v", err)
			return nil, err
		}
		if needsUpdate {
			// Ensure we preserve the ID when updating
			return c.srv.Events.Update(c.calendarID, existingEvent.Id, event).Do()
		}
		return existingEvent, nil
	}

	return c.srv.Events.Insert(c.calendarID, event).Do()
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
