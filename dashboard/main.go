package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
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

// url de layer do obs do soundalerts.com
// https://source.soundalerts.com/alert/f168212e-1e46-506e-b553-016c1d05e665
//		?layer-name=SoundAlerts
//		&layer-width=800
//		&layer-height=600

//go:embed .oauth_client_id
var clientID string

//go:embed .oauth_client_secret
var clientSecret string

//go:embed index.html
var indexHtml string

//go:embed layer.html
var layerHtml string

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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

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
		log.Printf("HandleRoot > corrupted session %s -- generated new", err)
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
		log.Println("HandleRoot > oauthToken is not *helix.AccessCredentials")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	client, err := helix.NewClient(&helix.Options{ClientID: clientID})
	if err != nil {
		log.Println("HandleRoot > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	client.SetUserAccessToken(token.AccessToken)

	user, err := client.GetUsers(nil)
	if err != nil {
		log.Println("HandleRoot > client.GetUsers:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(user.Data.Users) == 0 {
		log.Println("HandleRoot > len(userData.Users):", len(user.Data.Users))
		return
	}
	log.Println("HandleRoot > channel:", user.Data.Users[0].ID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("index").Parse(indexHtml)
	if err != nil {
		log.Printf("HandleRoot > error creating template: %v", err)
		return
	}
	parsed := bytes.NewBufferString("")
	vars := struct {
		UserID string
	}{
		UserID: user.Data.Users[0].ID,
	}
	err = tmpl.Execute(parsed, vars)
	if err != nil {
		log.Printf("HandleRoot > error parsing html: %v", err)
		return
	}
	_, err = w.Write(parsed.Bytes())
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

func HandleLayer(w http.ResponseWriter, r *http.Request) {
	log.Println("HandleLayer > URL:", r.URL)
	split := strings.Split(r.URL.Path, "/")
	if len(split) != 4 {
		log.Println("HandleLayer > len(split):", len(split))
		return
	}
	userID := split[2]
	log.Println("HandleLayer > userID:", userID)

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("layer").Parse(layerHtml)
	if err != nil {
		log.Printf("HandleLayer > error creating template: %v", err)
		return
	}
	parsed := bytes.NewBufferString("")
	vars := struct {
		UserID string
	}{
		UserID: userID,
	}
	err = tmpl.Execute(parsed, vars)
	if err != nil {
		log.Printf("HandleLayer > error parsing html: %v", err)
		return
	}
	_, err = w.Write(parsed.Bytes())
}

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	log.Println("HandleWebsocket > URL:", r.URL)
	split := strings.Split(r.URL.Path, "/")
	if len(split) != 3 {
		log.Println("HandleWebsocket > len(split):", len(split))
		return
	}
	userID := split[2]

	// Upgrade HTTP connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("HandleWebsocket: Upgrade error:", err)
		return
	}

	const (
		writeWait  = 10 * time.Second    // Time allowed to read the data from the client.
		pongWait   = 60 * time.Second    // Time allowed to read the next pong message from the client.
		pingPeriod = (pongWait * 9) / 10 // Send pings to client with this period. Must be less than pongWait.
	)
	pingTicker := time.NewTicker(pingPeriod)
	defer func() {
		log.Println("WS Handler: Exiting from wsHandler")
		pingTicker.Stop()
		_ = conn.Close()
	}()

	for {
		select {
		// COLA: https://github.com/gorilla/websocket/blob/master/examples/chat/client.go
		//case message, ok := <-c.send:
		//	conn.SetWriteDeadline(time.Now().Add(writeWait))
		//	if !ok {
		//		// The hub closed the channel.
		//		conn.WriteMessage(websocket.CloseMessage, []byte{})
		//		return
		//	}
		//
		//	w, err := conn.NextWriter(websocket.TextMessage)
		//	if err != nil {
		//		return
		//	}
		//	w.Write(message)
		//
		//	// Add queued chat messages to the current websocket message.
		//	n := len(c.send)
		//	for i := 0; i < n; i++ {
		//		w.Write(newline)
		//		w.Write(<-c.send)
		//	}
		//
		//	if err := w.Close(); err != nil {
		//		return
		//	}
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
	log.Println("HandleWebsocket: conectado:", conn.LocalAddr(), userID)
}

func main() {
	// Gob encoding for helix/AccessCredentials
	gob.Register(&helix.AccessCredentials{})

	mux := http.DefaultServeMux
	mux.HandleFunc("/login", HandleLogin)
	mux.HandleFunc("/logout", HandleLogout)
	mux.HandleFunc("/redirect", HandleOAuth2Callback)
	mux.HandleFunc("/layer/", HandleLayer)
	mux.HandleFunc("/ws/", HandleWebsocket)
	mux.HandleFunc("/", HandleRoot)

	fmt.Println("Started running on http://localhost:7001")
	fmt.Println(http.ListenAndServe(":7001", nil))
}
