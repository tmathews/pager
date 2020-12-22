package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tmathews/windows-toast"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Service struct {
	Credentials    *oauth2.Config
	Config         Config
	GoogleServices []*GoogleService
	Log            *log.Logger
}

func NewService(filename string) (*Service, error) {
	credentials, err := google.ConfigFromJSON(GoogleCredentials, GoogleScopes...)
	if err != nil {
		return nil, err
	}
	config, err := ReadConfig(filename)
	if err != nil {
		return nil, err
	}
	var xs []*GoogleService
	for _, service := range config.GoogleServices {
		gs, err := service.NewGoogleService(credentials)
		if err != nil {
			return nil, err
		}
		xs = append(xs, gs)
	}
	return &Service{
		Credentials:    credentials,
		Config:         config,
		GoogleServices: xs,
		Log:            log.New(os.Stdout, "", log.LstdFlags),
	}, nil
}

func (s *Service) Run(stop chan bool) {
	mailTime := time.Time{}
	calTime := time.Time{}
	last := time.Now()
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-time.Tick(time.Second):
			// It's important we don't change this as it reflects the run time we should be hitting
			now := time.Now()

			// Update our cached information from the servers
			if now.Sub(mailTime) >= s.Config.PollDeltaEmail {
				s.PollMails()
				mailTime = time.Now()
			}
			if now.Sub(calTime) >= s.Config.PollDeltaCalendar {
				s.PollEvents()
				calTime = time.Now()
			}

			// Let's find mail & events to print
			var ms []Mail
			var es []Event
			for _, g := range s.GoogleServices {
				for i, m := range g.Mails {
					if !m.Seen {
						g.Mails[i].Seen = true
						ms = append(ms, m)
					}
				}
				for i, xs := range g.Events {
					for id, e := range xs {
						for marker, seen := range e.Markers {
							if seen {
								continue
							}
							mtime := e.Start.Add(-1 * marker)
							if mtime.After(last) && (mtime.Before(now) || mtime.Equal(now)) {
								es = append(es, e)
								g.Events[i][id].Markers[marker] = true
							}
						}
					}
				}
			}
			s.PrintMails(ms)
			s.PrintEvents(es)
			last = now
		}
	}
}

// Blindly save the tokens to the config in the case they have been updated from the services.
func (s *Service) SaveTokens(filename string) {
	// We want a fresh load so that we don't overwrite any user changes
	c, err := ReadConfig(filename)
	if err != nil {
		s.Log.Println(err)
		return
	}
	// We'll update the token strings, kinda expecting no issues - but record them if it does
	for _, g := range s.GoogleServices {
		for _, gsc := range c.GoogleServices {
			if gsc.Name == g.Name {
				err := gsc.UpdateToken(g.Token)
				if err != nil {
					s.Log.Println(err)
				}
			}
		}
	}
	err = WriteConfig(filename, c)
	if err != nil {
		s.Log.Println(err)
	}
}

func (s *Service) PollMails() {
	for _, g := range s.GoogleServices {
		if g.Gmail == nil {
			continue
		}
		if err := g.GetNewMessages(); err != nil {
			s.Log.Printf("[%s] GetNewMessages error: %s", g.Name, err.Error())
		}
	}
}

func (s *Service) PollEvents() {
	for _, g := range s.GoogleServices {
		if g.Calendar == nil {
			continue
		}
		err := g.RefreshAllAgendas()
		if err != nil {
			s.Log.Printf("[%s] RefreshAllAgendas error: %s", g.Name, err.Error())
		}
		fmt.Println(g.Events)
	}
}

func (s *Service) PrintMails(xs []Mail) {
	var title, body string
	if len(xs) == 0 {
		return
	} else if len(xs) == 1 {
		mail := xs[0]
		title = fmt.Sprintf("📫 %s", mail.From)
		body = mail.Subject
	} else {
		title = fmt.Sprintf("📫 %d New Emails", len(xs))
		for _, y := range xs {
			body += fmt.Sprintf("📧 %s\n", y.Subject)
		}
	}
	s.Notify(title, body, "https://gmail.com", toast.Mail)
}

func (s *Service) PrintEvents(xs []Event) {
	var title, body string
	if len(xs) == 0 {
		return
	} else if len(xs) == 1 {
		event := xs[0]
		title = fmt.Sprintf("📅 %s", event.Title)
	} else {
		title = fmt.Sprintf("📅 %d Events Today", len(xs))
		for _, x := range xs {
			body += fmt.Sprintf("%s 🕛 %s\n", x.Start.Format(time.Kitchen), x.Title)
		}
	}
	s.Notify(title, body, "https://calendar.google.com", toast.Reminder)
}

func (s *Service) Notify(title, body, action string, sound toast.Audio) {
	n := toast.Notification{
		AppID:               "Pager",
		Title:               title,
		Message:             body,
		Audio:               sound,
		ActivationArguments: action,
	}
	err := n.Push()
	if err != nil {
		s.Log.Printf("Notify Error: %s", err.Error())
	} else {
		s.Log.Printf("Notified\n%s\n%s", title, body)
	}
}
