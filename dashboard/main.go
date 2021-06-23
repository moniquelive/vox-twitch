package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/nicklaw5/helix"
)

// soundalerts.com pede:
//Manage Channel Points custom rewards and their redemptions on your channel
//View Channel Points custom rewards and their redemptions on your channel
//Obtain your email address

// url de login da soundalerts.com:
// https://id.twitch.tv/oauth2/authorize
//         ?client_id=bttsqjy6dnv05acplp5vy0mflgrh3z
//         &redirect_uri=https://dashboard.soundalerts.com
//         &response_type=code
//         &scope=user:read:email+channel:manage:redemptions+channel:read:redemptions
//         &state=8q75cx

//go:embed .oauth_client_id
var clientID string

//go:embed .oauth_client_secret
var clientSecret string

const (
	oauthSessionName = "oauth-session"
	oauthTokenKey    = "oauth-token"
)

var (
	scopes       = []string{"user:read:email"}
	redirectURL  = "http://localhost:7001/redirect"
	cookieSecret = []byte("my awesome cookie secret <3 monique.dev")
	cookieStore  = sessions.NewCookieStore(cookieSecret)
)

// HandleRoot is a Handler that shows a login button. In production, if the frontend is served / generated
// by Go, it should use html/template to prevent XSS attacks.
func HandleRoot(w http.ResponseWriter, r *http.Request) {
	log.Println("URL:", r.URL)
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Printf("corrupted session %s -- generated new", err)
	}

	oauthToken := session.Values[oauthTokenKey]
	if oauthToken == nil {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, err = w.Write(
			[]byte(`<html><body><a href="/login">Login using Twitch</a></body></html>`))
		return
	}
	var ok bool
	token, ok := oauthToken.(*helix.AccessCredentials)
	if !ok {
		log.Println("oauthToken is not *helix.AccessCredentials")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID: clientID,
	})
	if err != nil {
		log.Println("NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	client.SetUserAccessToken(token.AccessToken)

	user, err := client.GetUsers(nil)
	if err != nil {
		log.Println("client.GetUsers:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(user.Data.Users) > 0 {
		log.Println("channel:", user.Data.Users[0].ID)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write([]byte((`<html><body><a href="/logout">Logout</a></body></html>`)))
}

// HandleLogin is a Handler that redirects the user to Twitch for login, and provides the 'state'
// parameter which protects against login CSRF.
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:    clientID,
		RedirectURI: redirectURL,
	})
	if err != nil {
		log.Println("NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// cria state token
	var tokenBytes [255]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		log.Println("Couldn't generate a token!", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(tokenBytes[:])
	url := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
		ResponseType: "code", // or "token"
		Scopes:       scopes,
		State:        state,
		ForceVerify:  false,
	})

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	return
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Println("cookieStore.Get:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// expira cookies
	session.Options.MaxAge = -1
	if err = session.Save(r, w); err != nil {
		log.Println("session.Save:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleOauth2Callback is a Handler for oauth's 'redirect_uri' endpoint;
// it validates the state token and retrieves an OAuth token from the request parameters.
func HandleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURL,
	})
	if err != nil {
		log.Println("NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	code := r.FormValue("code")

	token, err := client.RequestUserAccessToken(code)
	if err != nil {
		log.Println("client.RequestUserAccessToken:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Printf("corrupted session %s -- generated new", err)
		err = nil
	}
	session.Values[oauthTokenKey] = token.Data
	if err = session.Save(r, w); err != nil {
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	return
}

func main() {
	// Gob encoding for helix/AccessCredentials
	gob.Register(&helix.AccessCredentials{})

	mux := http.DefaultServeMux
	mux.HandleFunc("/", HandleRoot)
	mux.HandleFunc("/login", HandleLogin)
	mux.HandleFunc("/logout", HandleLogout)
	mux.HandleFunc("/redirect", HandleOAuth2Callback)

	fmt.Println("Started running on http://localhost:7001")
	fmt.Println(http.ListenAndServe(":7001", nil))
}
