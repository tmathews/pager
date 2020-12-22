package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	ErrNoProfile = errors.New("profile does not exist")
	ErrNoToken   = errors.New("no auth token")
)

type Config struct {
	PollDeltaEmail    time.Duration
	PollDeltaCalendar time.Duration
	GoogleServices    []GoogleServiceConfig
}

// Provides sane defaults in the case user edits to ridiculous values or there are none provided
func (c *Config) ApplyDefaults() {
	if c.PollDeltaCalendar <= time.Second {
		c.PollDeltaCalendar = time.Second
	}
	if c.PollDeltaEmail <= time.Second {
		c.PollDeltaEmail = time.Second
	}
}

func (c *Config) UpdateToken(profileName string, token *oauth2.Token) error {
	buf := bytes.NewBuffer([]byte{})
	err := json.NewEncoder(buf).Encode(&token)
	if err != nil {
		return err
	}
	for _, x := range c.GoogleServices {
		if x.Name == profileName {
			x.Token = buf.String()
			return nil
		}
	}
	return ErrNoProfile
}

func (c *Config) RemoveProfile(profileName string) bool {
	var found bool
	var xs []GoogleServiceConfig
	for _, v := range c.GoogleServices {
		if v.Name == profileName {
			found = true
			continue
		}
		xs = append(xs, v)
	}
	c.GoogleServices = xs
	return found
}

// Replaces the Google profile if it exists, otherwise it will append it.
func (c *Config) ReplaceProfile(profile GoogleServiceConfig) {
	for i, v := range c.GoogleServices {
		if v.Name == profile.Name {
			c.GoogleServices[i] = profile
			return
		}
	}
	c.GoogleServices = append(c.GoogleServices, profile)
}

type GoogleServiceConfig struct {
	Name      string
	Token     string
	Calendars []string
}

func (gsc *GoogleServiceConfig) NewGoogleService(config *oauth2.Config) (*GoogleService, error) {
	token, err := gsc.NewToken()
	if err != nil {
		return nil, err
	}
	client := config.Client(context.Background(), token)
	m, err := gmail.New(client)
	if err != nil {
		return nil, err
	}
	c, err := calendar.New(client)
	if err != nil {
		return nil, err
	}
	return &GoogleService{
		Name:      gsc.Name,
		Token:     token,
		Gmail:     m,
		Calendar:  c,
		Calendars: gsc.Calendars,
		Events:    make(map[string][]Event),
	}, nil
}

func (gsc *GoogleServiceConfig) NewToken() (*oauth2.Token, error) {
	var tok oauth2.Token
	err := json.NewDecoder(bytes.NewBufferString(gsc.Token)).Decode(&tok)
	return &tok, err
}

func (gsc *GoogleServiceConfig) UpdateToken(token *oauth2.Token) error {
	buf := bytes.NewBuffer([]byte{})
	err := json.NewEncoder(buf).Encode(&token)
	if err != nil {
		return err
	}
	gsc.Token = buf.String()
	return nil
}

// Tries to get the config for our app based on Windows AppData folders. Returns empty string if nothing could be
// formulated correctly.
func GetConfigFilename() string {
	str, ok := os.LookupEnv("APPDATA")
	if !ok {
		return ""
	}
	return filepath.Join(str, "Pager", "config.toml")
}

func ReadConfig(filename string) (Config, error) {
	var c Config
	_, err := toml.DecodeFile(filename, &c)
	c.ApplyDefaults()
	return c, err
}

func WriteConfig(filename string, c Config) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(&c)
}
