package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	cmd "github.com/tmathews/commander"
	toast "github.com/tmathews/windows-toast"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

var GoogleCredentials []byte

const ServiceName = "Pager"

func main() {
	is, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("Failed IsWindowsService check: %s", err.Error())
		return
	} else if is {
		runService(false)
		return
	}

	var args []string
	if len(os.Args) >= 2 {
		args = os.Args[1:]
	}
	err = cmd.Exec(args, cmd.Manual("Welcome to Pager", "Let's get our emails!\n"), cmd.M{
		"setup":        cmdSetup,
		"profile":      cmdProfile,
		"authenticate": cmdAuth,
		"service":      cmdService,
		"test-toast":   cmdTest,
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

func runService(fake bool) error {
	var fn func(string, svc.Handler) error
	var l debug.Log
	var err error
	if fake {
		fn = debug.Run
		l = debug.New(ServiceName)
	} else {
		fn = svc.Run
		l, err = eventlog.Open(ServiceName)
		if err != nil {
			return err
		}
	}
	defer l.Close()
	return fn("Pager", &Service{
		Log: l,
	})
}

func cmdSetup(name string, args []string) error {
	var remove bool
	set := flag.NewFlagSet(name, flag.ExitOnError)
	set.BoolVar(&remove, "uninstall", false, "Uninstall the service.")
	set.Usage = func() {
		fmt.Println("pager setup [-flags...]")
		set.PrintDefaults()
	}
	if err := set.Parse(args); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)

	// Handle the case where we want to remove the service or check if it exists if we installing
	if err == nil && !remove {
		return errors.New("service already exists")
	} else if err != nil && remove {
		return err
	} else if s != nil && remove {
		eventlog.Remove(ServiceName)
		s.Delete()
		s.Close()
		return nil
	}

	exPath, err := exePath()
	if err != nil {
		return fmt.Errorf("executable path not found: %s", err.Error())
	}

	// Install the service & logging system
	s, err = m.CreateService(ServiceName, exPath, mgr.Config{
		DisplayName: ServiceName,
		Description: "Google Pager application for showing Gmail & Calendar notifications.",
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	err = eventlog.InstallAsEventCreate(ServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return errors.New("create event logger failed")
	}
	return nil
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
	set.StringVar(&cfgFilename, "c", GetConfigFilename(), "Configuration file to save to.")
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

	for i, profile := range config.GoogleServices {
		if profile.Name != profileName {
			continue
		}
		if err := authProfile(&profile); err != nil {
			return err
		}
		config.GoogleServices[i] = profile
		break
	}

	err = WriteConfig(cfgFilename, config)
	if err != nil {
		return err
	}
	fmt.Println("Configuration updated.")
	return nil
}

func cmdService(_ string, _ []string) error {
	return runService(true)
}

func cmdTest(_ string, _ []string) error {
	return Notify("Test", "If you see this notification, the test has been successful.", "", toast.Default)
}

func authProfile(profile *GoogleServiceConfig) error {
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

	return profile.UpdateToken(token)
}

func exePath() (string, error) {
	p, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", err
	}
	check := func(p string) error {
		fi, err := os.Stat(p)
		if err == nil && fi.IsDir() {
			return fmt.Errorf("%s is directory", p)
		}
		return err
	}
	// First check if the original path provided is a binary
	if err = check(p); err == nil {
		return p, nil
	}
	// Let's assume they forgot the extension ands try that
	if filepath.Ext(p) == "" {
		p += ".exe"
		if err = check(p); err == nil {
			return p, nil
		}
	}
	return "", err
}
