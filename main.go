package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-toast/toast"
	cmd "github.com/tmathews/commander"
	"golang.org/x/oauth2"
)

func main() {
	var args []string
	if len(os.Args) >= 2 {
		args = os.Args[1:]
	}
	err := cmd.Exec(args, cmd.Manual("Welcome to Pager", "Let's get our emails!\n"), cmd.M{
		"auth": cmdAuth,
		"poll": cmdPoll,
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
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	if err := set.Parse(args); err != nil {
		return err
	}
	profileName := strings.TrimSpace(set.Arg(0))
	if profileName == "" {
		return errors.New("invalid profile name")
	}

	config, err := readConfig("credentials.json")
	if err != nil {
		return err
	}

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Authenticate: \n%v\n\nEnter your token.\n", authURL)
	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return err
	}
	token, err := config.Exchange(context.Background(), authCode)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(".", profileName+".json"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(token); err != nil {
		return err
	}
	fmt.Println("Token saved.")
	return nil
}

func cmdPoll(name string, args []string) error {
	var delta time.Duration
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.DurationVar(&delta, "t", time.Minute * 5, "The amount of time between polls.")
	if err := set.Parse(args); err != nil {
		return err
	}
	profileName := strings.TrimSpace(set.Arg(0))
	if profileName == "" {
		return errors.New("invalid profile name")
	}

	gmail, err := NewGmail("credentials.json", profileName+".json")
	if err != nil {
		panic(err)
	}

	for {
		PollGmail(gmail)
		time.Sleep(delta)
	}
	return nil
}

func PollGmail(gmail *Gmail) {
	xs, err := gmail.GetNewMessages()
	if err != nil {
		log.Println(err.Error())
		return
	}
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
		log.Printf("Notified: %s, %s", title, body)
	}
	return err
}
