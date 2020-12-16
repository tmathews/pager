package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	cmd "github.com/tmathews/commander"
	"github.com/tmathews/windows-toast"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sys/windows/svc"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

var GoogleCredentials []byte

func main() {
	var args []string
	if len(os.Args) >= 2 {
		args = os.Args[1:]
	}
	err := cmd.Exec(args, cmd.Manual("Welcome to Pager", "Let's get our emails!\n"), cmd.M{
		"auth":   cmdAuth,
		"daemon": cmdDaemon,
	})
	if err != nil {
		switch v := err.(type) {
		case cmd.Error:
			fmt.Print(v.Help())
			os.Exit(2)
		default:
			fmt.Println(err.Error())
			os.Exit(1)
		}
	}
}

func cmdAuth(name string, args []string) error {
	var cfgFilename string
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.StringVar(&cfgFilename, "c", "config.toml", "Configuration file to save to.")
	if err := set.Parse(args); err != nil {
		return err
	}
	profileName := strings.TrimSpace(set.Arg(0))
	if profileName == "" {
		return errors.New("invalid profile name")
	}

	config, err := ReadConfig(cfgFilename)
	if err != nil {
		return err
	}

	credentials, err := google.ConfigFromJSON(GoogleCredentials, gmail.GmailReadonlyScope, calendar.CalendarReadonlyScope)
	if err != nil {
		return err
	}

	authURL := credentials.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Authenticate: \n%v\n\nEnter your token.\n", authURL)
	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return err
	}
	token, err := credentials.Exchange(context.Background(), authCode)
	if err != nil {
		return err
	}

	err = config.UpdateToken(profileName, token)
	if err != nil {
		return err
	}
	err = WriteConfig(cfgFilename, config)
	if err != nil {
		return err
	}
	fmt.Println("Configuration updated.")
	return nil
}

func cmdDaemon(name string, args []string) error {
	var cfgFilename string
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.StringVar(&cfgFilename, "c", "config.toml", "Configuration TOML file to load.")
	if err := set.Parse(args); err != nil {
		return err
	}

	credentials, err := google.ConfigFromJSON(GoogleCredentials, gmail.GmailReadonlyScope, calendar.CalendarReadonlyScope)
	if err != nil {
		return err
	}
	config, err := ReadConfig(cfgFilename)
	if err != nil {
		return err
	}

	var mailboxes []*Gmail
	var calendars []*Gcal
	emailCh := make(chan []Mail)
	eventCh := make(chan []Event)
	for _, s := range config.GoogleServices {
		token, err := s.NewToken()
		if err != nil {
			return err
		}
		gmail, err := s.NewGmail(credentials, token)
		if err != nil {
			return err
		}
		mailboxes = append(mailboxes, gmail)

		gcal, err := s.NewCalendar(credentials, token)
		if err != nil {
			return err
		}
		calendars = append(calendars, gcal)
	}

	// We like to poll all services at the same time to keep things in orderly fashion
	go RunMail(emailCh, mailboxes, config.PollDeltaEmail)
	go RunCalendars(eventCh, calendars, config.PollDeltaCalendar)

	log.Println("Here we go!")

	for {
		select {
		case ms := <-emailCh:
			PrintMails(ms)
		case es := <-eventCh:
			PrintEvents(es)
		}
	}
}

func RunMail(ch chan []Mail, mailboxes []*Gmail, delta time.Duration) chan bool {
	closer := make(chan bool)
	go func() {
	loop:
		for {
			var xs []Mail
			for _, g := range mailboxes {
				ys, err := g.GetNewMessages()
				if err != nil {
					log.Println(err)
				} else {
					xs = append(xs, ys...)
				}
			}
			if len(xs) > 0 {
				ch <- xs
			}
			select {
			case <- closer:
				break loop
			case <- time.After(delta):
				continue loop
			}
		}
	}()
	return closer
}

func RunCalendars(ch chan []Event, calendars []*Gcal, delta time.Duration) chan bool {
	closer := make(chan bool)
	go func() {
		started := time.Now()
	loop:
		for {
			now := time.Now()
			if now.Sub(started) >= delta {
				for _, y := range calendars {
					if err := y.RefreshAll(); err != nil {
						log.Println(err)
					}
				}
			}
			var xs []Event
			for _, y := range calendars {
				xs = append(xs, y.CheckAll(time.Second)...)
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
		AppID:   "Pager",
		Title:   title,
		Message: body,
		Audio:   sound,
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
