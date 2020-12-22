package main

import (
	"time"

	"golang.org/x/oauth2"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

type GoogleService struct {
	Name      string
	Token     *oauth2.Token
	Gmail     *gmail.Service
	Calendar  *calendar.Service
	Events    map[string]map[string]Event
	Mails     []Mail
	Calendars []string
}

type Mail struct {
	To        string
	From      string
	Subject   string
	Id        string
	Timestamp time.Time
	Seen      bool
}

type Event struct {
	Id      string
	Title   string
	Start   time.Time
	End     time.Time
	Markers map[time.Duration]bool
}

type Q string

func (q Q) Get() (string, string) {
	return "q", string(q)
}

func (g *GoogleService) GetNewMessages() error {
	user := "me"
	r, err := g.Gmail.Users.Messages.List(user).Do(Q("is:unread newer_than:1d"))
	if err != nil {
		return err
	}
loop:
	for _, message := range r.Messages {
		m, err := g.Gmail.Users.Messages.Get(user, message.Id).Do()
		if err != nil {
			return err
		}
		for _, x := range g.Mails {
			if x.Id == m.Id {
				continue loop
			}
		}

		x := Mail{
			Id:        m.Id,
			Timestamp: time.Unix(m.InternalDate/1000, 0),
		}
		for _, h := range m.Payload.Headers {
			switch h.Name {
			case "Subject":
				x.Subject = h.Value
			case "From":
				x.From = h.Value
			case "To":
				x.To = h.Value
			}
		}
		g.Mails = append(g.Mails, x)
	}
	return nil
}

func (g *GoogleService) ListCalendars() ([]string, error) {
	r, err := g.Calendar.CalendarList.List().Do()
	if err != nil {
		return nil, err
	}
	var xs []string
	for _, x := range r.Items {
		xs = append(xs, x.Id)
	}
	return xs, nil
}

func (g *GoogleService) RefreshAllAgendas() error {
	for _, x := range g.Calendars {
		if err := g.RefreshAgenda(x); err != nil {
			return err
		}
	}
	return nil
}

func (g *GoogleService) RefreshAgenda(id string) error {
	now := time.Now()
	r, err := g.Calendar.Events.List(id).
		TimeMin(now.Format(time.RFC3339)).
		TimeMax(time.Now().Add(time.Hour * 12).Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		Do()
	if err != nil {
		return err
	}
	markers := map[time.Duration]bool{0: false}
	for _, v := range r.DefaultReminders {
		markers[time.Duration(v.Minutes)*time.Minute] = false
	}

	var agenda map[string]Event
	if g.Events[id] == nil {
		agenda = make(map[string]Event)
	} else {
		agenda = g.Events[id]
	}
	for _, e := range r.Items {
		start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
		start = time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), start.Location())
		var end time.Time
		if !e.EndTimeUnspecified {
			end, _ = time.Parse(time.RFC3339, e.End.DateTime)
			end = time.Date(now.Year(), now.Month(), now.Day(), end.Hour(), end.Minute(), end.Second(), end.Nanosecond(), end.Location())
		}
		mxs := make(map[time.Duration]bool)
		if e.Reminders.UseDefault {
			for m, s := range markers {
				mxs[m] = s
			}
		}
		for _, v := range e.Reminders.Overrides {
			mxs[time.Duration(v.Minutes)*time.Minute] = false
		}

		// Update the event, via new event, and it's markers
		ev := Event{
			Id:      e.Id,
			Title:   e.Summary,
			Start:   start,
			End:     end,
			Markers: mxs,
		}
		if x, ok := agenda[ev.Id]; ok {
			for m, seen := range x.Markers {
				ev.Markers[m] = seen
			}
		}
		agenda[ev.Id] = ev
	}
	g.Events[id] = agenda
	return nil
}
