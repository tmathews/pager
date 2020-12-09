package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-toast/toast"
	cmd "github.com/tmathews/commander"
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
		"test":   cmdTest,
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

	var emailCh chan []Mail
	var eventCh chan []Event
	var closers []chan bool
	for _, s := range config.GoogleServices {
		token, err := s.GetToken()
		if err != nil {
			return err
		}
		gmail, err := NewGmail(credentials, token)
		if err != nil {
			return err
		}
		gmail.PollDelta = s.PollDeltaEmail
		closers = append(closers, gmail.Run(emailCh))

		gcal, err := NewGcal(credentials, token)
		gcal.PollDelta = s.PollDeltaCalendar
		closers = append(closers, gcal.Run(eventCh))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)
loop:
	for {
		select {
		case <-sig:
			break loop
		case ms := <-emailCh:
			PrintMails(ms)
		case es := <-eventCh:
			PrintEvents(es)
		}
	}

	for _, c := range closers {
		c <- true
		close(c)
	}

	return nil
}

func PrintMails(xs []Mail) {
	if len(xs) == 0 {
		return
	} else if len(xs) == 1 {
		mail := xs[0]
		Notify(fmt.Sprintf("📫 %s", mail.From), mail.Subject)
	} else if len(xs) > 1 {
		var body string
		for _, y := range xs {
			body += fmt.Sprintf("📧 %s\n", y.Subject)
		}
		Notify(fmt.Sprintf("📫 %d New Emails", len(xs)), body)
	}
}

func PrintEvents(xs []Event) {
	var str string
	for _, x := range xs {
		str += fmt.Sprintf("%s 🕛 %s\n", x.Start.Format(time.Kitchen), x.Title)
	}
	Notify(fmt.Sprintf("📅 %d Events Today", len(xs)), str)
}

func Notify(title, body string) error {
	n := toast.Notification{
		AppID:   "Pager",
		Title:   title,
		Message: body,
	}
	err := n.Push()
	if err != nil {
		log.Printf("Notify Error: %s", err.Error())
	} else {
		log.Printf("Notified\n%s\n%s", title, body)
	}
	return err
}

func cmdTest(name string, args []string) error {
	fmt.Println("Yay")
	return nil
}
