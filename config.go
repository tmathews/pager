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
	"time"

	"github.com/BurntSushi/toml"
)

var ErrNoProfile = errors.New("profile does not exist")

type Config struct {
	PollDeltaEmail    time.Duration
	PollDeltaCalendar time.Duration
	GoogleServices []GoogleServiceConfig
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

type GoogleServiceConfig struct {
	Name              string
	Token             string
	Calendars         []string
}

func (gs *GoogleServiceConfig) NewGmail(config *oauth2.Config, token *oauth2.Token) (*Gmail, error) {
	client := config.Client(context.Background(), token)
	srv, err := gmail.New(client)
	if err != nil {
		return nil, err
	}
	return &Gmail{
		Service:   srv,
	}, nil
}

func (gs *GoogleServiceConfig) NewCalendar(config *oauth2.Config, token *oauth2.Token) (*Gcal, error) {
	client := config.Client(context.Background(), token)
	srv, err := calendar.New(client)
	if err != nil {
		return nil, err
	}
	return &Gcal{
		Service:   srv,
		Events:    make(map[string][]Event),
		Calendars: gs.Calendars,
	}, nil
}

func (gs *GoogleServiceConfig) NewToken() (*oauth2.Token, error) {
	var tok oauth2.Token
	err := json.NewDecoder(bytes.NewBufferString(gs.Token)).Decode(&tok)
	return &tok, err
}

func ReadConfig(filename string) (Config, error) {
	var c Config
	_, err := toml.DecodeFile(filename, &c)
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
