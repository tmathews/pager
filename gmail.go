package main

import (
	"time"

	"google.golang.org/api/gmail/v1"
)

type Gmail struct {
	Service *gmail.Service

	// This is a list of all messages we have seen. We are just storing this in memory just because I don't want to
	// write to a file.
	Seen      []string
}

type Mail struct {
	To        string
	From      string
	Subject   string
	Id        string
	Timestamp time.Time
}

type Q string

func (q Q) Get() (string, string) {
	return "q", string(q)
}

func (g *Gmail) GetNewMessages() ([]Mail, error) {
	user := "me"
	r, err := g.Service.Users.Messages.List(user).Do(Q("is:unread newer_than:1d"))
	if err != nil {
		return nil, err
	}
	var xs []Mail
loop:
	for _, message := range r.Messages {
		m, err := g.Service.Users.Messages.Get(user, message.Id).Do()
		if err != nil {
			return nil, err
		}
		for _, x := range g.Seen {
			if x == m.Id {
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
		xs = append(xs, x)
		g.Seen = append(g.Seen, x.Id)
	}
	return xs, nil
}