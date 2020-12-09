package main

import (
	"context"
	"io/ioutil"
	"log"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

type Gmail struct {
	Service *gmail.Service

	// This is a list of all messages we have seen. We are just storing this in memory just because I don't want to
	// write to a file.
	Seen      []string
	PollDelta time.Duration
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

func ReadCredentials(filename string, scope ...string) (*oauth2.Config, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return google.ConfigFromJSON(b, scope...)
}

func NewGmail(config *oauth2.Config, token *oauth2.Token) (*Gmail, error) {
	client := config.Client(context.Background(), token)
	srv, err := gmail.New(client)
	if err != nil {
		return nil, err
	}
	return &Gmail{
		Service: srv,
	}, nil
}

func (g *Gmail) Run(ch chan []Mail) chan bool {
	closer := make(chan bool)
	go func() {
	loop:
		for {
			xs, err := g.GetNewMessages()
			if err == nil {
				ch <- xs
			} else {
				log.Println(err)
			}
			time.Sleep(g.PollDelta)
			select {
			case <- closer:
				break loop
			case <- time.After(g.PollDelta):
				continue loop
			}
		}
	}()
	return closer
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