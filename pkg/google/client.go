package google

import (
	"context"
	"fmt"

	"github.com/harrisonrobin/taska/pkg/auth"
	"github.com/harrisonrobin/taska/pkg/index"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// NewClient creates a new Google Calendar client.
func NewClient(calendarName string, idx *index.EventIndex) (*CalendarClient, error) {
	ctx := context.Background()
	scopes := []string{
		calendar.CalendarEventsScope,
		calendar.CalendarReadonlyScope,
	}
	client, err := auth.GetClient(ctx, scopes)
	if err != nil {
		return nil, err
	}

	srv, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Calendar client: %v", err)
	}

	calendarList, err := srv.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve calendar list: %v", err)
	}

	var calendarID string
	for _, item := range calendarList.Items {
		if item.Summary == calendarName {
			calendarID = item.Id
			break
		}
	}

	if calendarID == "" {
		return nil, fmt.Errorf("calendar '%s' not found", calendarName)
	}

	return NewCalendarClient(srv, calendarID, idx), nil
}
