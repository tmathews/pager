package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	cmd "github.com/tmathews/commander"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

var (
	GoogleCredentials []byte
	GoogleScopes      = []string{
		gmail.GmailReadonlyScope,
		calendar.CalendarEventsReadonlyScope,
	}
)

func main() {
	var args []string
	if len(os.Args) >= 2 {
		args = os.Args[1:]
	}
	err := cmd.Exec(args, cmd.Manual("Welcome to Pager", "Let's get our emails!\n"), cmd.M{
		"profile":      cmdProfile,
		"authenticate": cmdAuth,
		"run":          cmdRun,
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

func cmdProfile(name string, args []string) error {
	var cfgFilename, calendarStr string
	var autoCreate, del, auth bool
	set := flag.NewFlagSet(name, flag.ExitOnError)
	set.StringVar(&cfgFilename, "config", GetConfigFilename(), "Configuration file to save to.")
	set.BoolVar(&autoCreate, "auto-create", true, "Automatically create the config file if not found.")
	set.BoolVar(&del, "remove", false, "Remove the profile instead.")
	set.BoolVar(&auth, "auth", false, "Additionally run the authentication process.")
	set.StringVar(&calendarStr, "calendars", "", "A comma separated string of calendars to read.")
	set.Usage = func() {
		fmt.Println(`pager profile [-flags...] <profile_name>

<profile_name> A profile name which is to be updated or created.`)
		set.PrintDefaults()
	}
	if err := set.Parse(args); err != nil {
		return err
	}
	if del {
		autoCreate = false
	}
	profileName := strings.TrimSpace(set.Arg(0))
	if profileName == "" {
		return errors.New("invalid profile name")
	}
	conf, err := ReadConfig(cfgFilename)
	if err != nil && !(os.IsNotExist(err) && autoCreate) {
		return err
	}

	if del {
		conf.RemoveProfile(profileName)
		return WriteConfig(cfgFilename, conf)
	}

	var profile GoogleServiceConfig

	// Do updates if it exists
	for _, v := range conf.GoogleServices {
		if v.Name == profileName {
			profile = v
			break
		}
	}
	profile.Name = profileName

	// Clean up calendars from bad input
	if len(calendarStr) > 0 {
		var calendars []string
		calendars = strings.Split(calendarStr, ",")
		for i, v := range calendars {
			calendars[i] = strings.TrimSpace(v)
		}
		profile.Calendars = calendars
	}

	// Run auth process should they have specified it
	if auth {
		err := authProfile(&profile)
		if err != nil {
			return err
		}
	}

	conf.ReplaceProfile(profile)
	return WriteConfig(cfgFilename, conf)
}

func cmdAuth(name string, args []string) error {
	var cfgFilename string
	set := flag.NewFlagSet(name, flag.ExitOnError)
	set.StringVar(&cfgFilename, "config", GetConfigFilename(), "Configuration file to save to.")
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

	var found bool
	for i, profile := range config.GoogleServices {
		if profile.Name != profileName {
			continue
		}
		if err := authProfile(&profile); err != nil {
			return err
		}
		config.GoogleServices[i] = profile
		found = true
		break
	}
	if !found {
		return ErrNoProfile
	}

	err = WriteConfig(cfgFilename, config)
	if err != nil {
		return err
	}
	fmt.Println("Configuration updated.")
	return nil
}

func cmdRun(name string, args []string) error {
	var cfgFilename string
	set := flag.NewFlagSet(name, flag.ExitOnError)
	set.StringVar(&cfgFilename, "config", GetConfigFilename(), "Configuration file to save to.")
	if err := set.Parse(args); err != nil {
		return err
	}

	s, err := NewService(cfgFilename)
	if err != nil {
		return err
	}

	s.Log.Println("Running.")
	stop := make(chan bool)
	sig := make(chan os.Signal)
	signal.Notify(sig)
	go s.Run(stop)
loop:
	for {
		select {
		case <-sig:
			stop <- true
			break loop
		}
	}
	s.Log.Println("Exiting.")
	return nil
}

func authProfile(profile *GoogleServiceConfig) error {
	credentials, err := google.ConfigFromJSON(GoogleCredentials, GoogleScopes...)
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

	return profile.UpdateToken(token)
}
