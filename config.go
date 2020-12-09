package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

var ErrNoProfile = errors.New("profile does not exist")

type Config struct {
	GoogleServices []GoogleServiceConfig
}

type GoogleServiceConfig struct {
	Name              string
	Token             string
	Calendars         []string
	PollDeltaEmail    time.Duration
	PollDeltaCalendar time.Duration
}

func (gs *GoogleServiceConfig) GetToken() (*oauth2.Token, error) {
	var tok *oauth2.Token
	err := json.NewDecoder(bytes.NewBufferString(gs.Token)).Decode(tok)
	return tok, err
}

/*func (c *Config) GetProfile(profileName string) *GoogleServiceConfig {
	for _, x := range c.GoogleServices {
		if x.Name == profileName {
			return &x
		}
	}
	return nil
}*/

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
