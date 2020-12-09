package main

import (
	"context"
	"fmt"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"log"
	"time"
)

type Gcal struct {
	Service    *calendar.Service
	Events     map[string][]Event
	PollDelta  time.Duration
	Calendars  []string
}

type Event struct {
	Id    string
	Title string
	Start time.Time
	End   time.Time
}

func NewGcal(config *oauth2.Config, token *oauth2.Token) (*Gcal, error) {
	client := config.Client(context.Background(), token)
	srv, err := calendar.New(client)
	if err != nil {
		return nil, err
	}
	return &Gcal{
		Service: srv,
		Events:  make(map[string][]Event),
	}, nil
}

func (g *Gcal) Run(ch chan []Event) chan bool {
	closer := make(chan bool)
	go func() {
		started := time.Now()
	loop:
		for {
			now := time.Now()
			if now.Sub(started) >= g.PollDelta {
				for _, x := range g.Calendars {
					err := g.RefreshAgenda(x)
					if err != nil {
						log.Println(err)
					}
				}
			}
			var xs []Event
			for _, x := range g.Calendars {
				xs = append(xs, g.CheckEvents(x, time.Second)...)
			}
			if len(xs) > 0 {
				ch <- xs
			}
			select {
			case <-closer:
				break loop
			case <-time.After(time.Second):
				continue loop
			}
		}
	}()
	return closer
}

func (g *Gcal) ListCalendars() ([]string, error) {
	r, err := g.Service.CalendarList.List().Do()
	if err != nil {
		return nil, err
	}
	var xs []string
	for _, x := range r.Items {
		xs = append(xs, x.Id)
	}
	return xs, nil
}

func (g *Gcal) RefreshAgenda(id string) error {
	now := time.Now()
	r, err := g.Service.Events.List(id).TimeMin(now.Format(time.RFC3339)).TimeMax(time.Now().Add(time.Hour * 12).Format(time.RFC3339)).SingleEvents(true).OrderBy("startTime").Do()
	if err != nil {
		return err
	}

	g.Events[id] = make([]Event, len(r.Items))
	for i, e := range r.Items {
		fmt.Println(e.Id, e.Summary, e.Start, e.End)
		start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
		start = time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), start.Location())
		var end time.Time
		if !e.EndTimeUnspecified {
			end, _ = time.Parse(time.RFC3339, e.End.DateTime)
			end = time.Date(now.Year(), now.Month(), now.Day(), end.Hour(), end.Minute(), end.Second(), end.Nanosecond(), end.Location())
		}
		g.Events[id][i] = Event{
			Id:    e.Id,
			Title: e.Summary,
			Start: start,
			End:   end,
		}
	}
	return nil
}

func (g *Gcal) CheckEvents(id string, d time.Duration) []Event {
	var xs []Event
	now := time.Now()
	for _, e := range g.Events[id] {
		if (now.Before(e.Start) || now.Equal(e.Start)) && e.Start.Sub(now) < d {
			xs = append(xs, e)
		}
	}
	return xs
}
