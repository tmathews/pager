package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	toast "github.com/tmathews/windows-toast"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

type Service struct {
	Credentials    *oauth2.Config
	Config         Config
	GoogleServices []*GoogleService
	Log            debug.Log
}

func (s *Service) Init(filename string) error {
	credentials, err := google.ConfigFromJSON(GoogleCredentials, gmail.GmailReadonlyScope, calendar.CalendarReadonlyScope)
	if err != nil {
		return err
	}

	config, err := ReadConfig(filename)
	if err != nil {
		return err
	}
	s.Credentials = credentials
	s.Config = config

	for _, service := range config.GoogleServices {
		gs, err := service.NewGoogleService(credentials)
		if err != nil {
			return err
		}
		s.GoogleServices = append(s.GoogleServices, gs)
	}
	return nil
}

func (s *Service) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	if len(args) > 1 {
		args = args[1:]
	} else {
		args = []string{}
	}
	var cfgFilename string
	set := flag.NewFlagSet("pager", flag.ContinueOnError)
	set.StringVar(&cfgFilename, "config", GetConfigFilename(), "Configuration file location.")
	if err := set.Parse(args); err != nil {
		s.Log.Error(1, fmt.Sprintf("Failed arguments: %s", err.Error()))
		return true, 2
	}
	if err := s.Init(cfgFilename); err != nil {
		s.Log.Error(1, fmt.Sprintf("Failed Init: %s", err.Error()))
		return true, 1
	}

	const accepted = svc.AcceptShutdown | svc.AcceptStop | svc.AcceptPauseAndContinue
	status <- svc.Status{State: svc.Running, Accepts: accepted}
	stop := make(chan bool)
	go s.Run(time.Time{}, stop)
	s.Log.Info(1, "Running.")

loop:
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Pause:
				stop <- true
				status <- svc.Status{State: svc.Paused, Accepts: accepted}
				s.Log.Info(1, "Paused.")
			case svc.Stop, svc.Shutdown:
				stop <- true
				s.Log.Info(1, fmt.Sprintf("Got halting signal: %d", c.Cmd))
				break loop
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: accepted}
				s.Log.Info(1, "Continuing.")
				go s.Run(time.Now(), stop)
			}
		}
	}
	status <- svc.Status{State: svc.StopPending}
	s.Log.Info(1, "Exiting...")
	s.SaveTokens(cfgFilename)
	return true, 0
}

func (s *Service) Run(start time.Time, stop chan bool) {
	mailTime := start
	calTime := start
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

			// Let's go through everything we haven't seen or is happening right now and print it
			var ms []Mail
			var es []Event
			for _, g := range s.GoogleServices {
				for i, m := range g.Mails {
					if !m.Seen {
						g.Mails[i].Seen = true
						ms = append(ms, m)
					}
				}
				for _, xs := range g.Events {
					for _, e := range xs {
						if now.Sub(e.Start) <= time.Second {
							es = append(es, e)
						}
					}
				}
			}
			PrintMails(ms)
			PrintEvents(es)
		}
	}
}

// Blindly save the tokens to the config in the case they have been updated from the services.
func (s *Service) SaveTokens(filename string) {
	// We want a fresh load so that we don't overwrite any user changes
	c, err := ReadConfig(filename)
	if err != nil {
		s.Log.Error(1, err.Error())
		return
	}
	// We'll update the token strings, kinda expecting no issues - but record them if it does
	for _, g := range s.GoogleServices {
		for _, gsc := range c.GoogleServices {
			if gsc.Name == g.Name {
				err := gsc.UpdateToken(g.Token)
				if err != nil {
					s.Log.Error(1, err.Error())
				}
			}
		}
	}
	err = WriteConfig(filename, c)
	if err != nil {
		s.Log.Error(1, err.Error())
	}
}

func (s *Service) PollMails() {
	for _, g := range s.GoogleServices {
		if g.Gmail == nil {
			continue
		}
		if err := g.GetNewMessages(); err != nil {
			s.Log.Error(1, fmt.Sprintf("[%s] GetNewMessages error: %s", g.Name, err.Error()))
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
			s.Log.Error(1, fmt.Sprintf("[%s] RefreshAllAgendas error: %s", g.Name, err.Error()))
		}
	}
}

func PrintMails(xs []Mail) {
	if len(xs) == 0 {
		return
	} else if len(xs) == 1 {
		mail := xs[0]
		Notify(fmt.Sprintf("📫 %s", mail.From), mail.Subject, "https://gmail.com", toast.Mail)
		return
	}
	var body string
	for _, y := range xs {
		body += fmt.Sprintf("📧 %s\n", y.Subject)
	}
	Notify(fmt.Sprintf("📫 %d New Emails", len(xs)), body, "https://gmail.com", toast.Mail)
}

func PrintEvents(xs []Event) {
	if len(xs) == 0 {
		return
	} else if len(xs) == 1 {
		event := xs[0]
		Notify(fmt.Sprintf("📅 %s", event.Title), "", "https://calendar.google.com", toast.Reminder)
		return
	}
	var str string
	for _, x := range xs {
		str += fmt.Sprintf("%s 🕛 %s\n", x.Start.Format(time.Kitchen), x.Title)
	}
	Notify(fmt.Sprintf("📅 %d Events Today", len(xs)), str, "https://calendar.google.com", toast.Reminder)
}

func Notify(title, body, action string, sound toast.Audio) error {
	n := toast.Notification{
		AppID:               "Pager",
		Title:               title,
		Message:             body,
		Audio:               sound,
		ActivationArguments: action,
	}
	err := n.Push()
	if err != nil {
		log.Printf("Notify Error: %s", err.Error())
	} else {
		log.Printf("Notified\n%s\n%s", title, body)
	}
	return err
}
