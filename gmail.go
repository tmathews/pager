package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

type Gmail struct {
	Service *gmail.Service

	// This is a list of all messages we have seen. We are just storing this in memory just because I don't want to
	// write to a file.
	Seen []string
}

type Mail struct {
	To        string
	From      string
	Subject   string
	Id        string
	Timestamp time.Time
}

func readConfig(filename string) (*oauth2.Config, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
}

func NewGmail(filename, token string) (*Gmail, error) {
	config, err := readConfig(filename)
	if err != nil {
		return nil, err
	}
	tok, err := tokenFromFile(token)
	if err != nil {
		return nil, err
	}
	client := config.Client(context.Background(), tok)
	srv, err := gmail.New(client)
	return &Gmail{
		Service: srv,
	}, nil
}

func (g *Gmail) GetNewMessages() ([]Mail, error) {
	user := "me"
	r, err := g.Service.Users.Messages.List(user).Do(Q("is:unread newer_than:1d"))
	if err != nil {
		return nil, err
	}
	var xs []Mail
loop:
	for _, message := range r.Messages {
		m, err := g.Service.Users.Messages.Get(user, message.Id).Do()
		if err != nil {
			return nil, err
		}
		for _, x := range g.Seen {
			if x == m.Id {
				continue loop
			}
		}

		x := Mail{
			Id:        m.Id,
			Timestamp: time.Unix(m.InternalDate/1000, 0),
		}
		for _, h := range m.Payload.Headers {
			switch h.Name {
			case "Subject":
				x.Subject = h.Value
			case "From":
				x.From = h.Value
			case "To":
				x.To = h.Value
			}
		}
		xs = append(xs, x)
		g.Seen = append(g.Seen, x.Id)
	}
	return xs, nil
}

type Q string

func (q Q) Get() (string, string) {
	return "q", string(q)
}

func getClient(config *oauth2.Config, tokFile string) *http.Client {
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}
