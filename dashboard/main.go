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

	"github.com/golang-jwt/jwt"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/moniquelive/vox-twitch/dashboard/cybervox"
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

//go:embed .vox_client_id
var voxClientID string

//go:embed .vox_client_secret
var voxClientSecret string

//go:embed logged-out.html
var loggedOutHTML []byte

//go:embed logged-in.html
var loggedInHTML string

//go:embed layer.html
var layerHtml string

//go:embed elm/elm.min.js
var elmMinJs []byte

const (
	oauthSessionName = "oauth-session"
	oauthTokenKey    = "oauth-token"
)

var (
	scopes       = []string{"user:read:email"}
	redirectURL  = "https://vox-twitch.monique.dev/redirect" //"http://localhost:7001/redirect"
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
		_, err = w.Write(loggedOutHTML)
		return
	}
	var ok bool
	token, ok := oauthToken.(*helix.AccessCredentials)
	if !ok {
		log.Println("HandleRoot > oauthToken is not *helix.AccessCredentials")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		log.Println("HandleRoot > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	validateToken, _, _ := client.ValidateToken(token.AccessToken)
	if !validateToken {
		if token, err = refreshToken(w, r, client, token.RefreshToken); err != nil {
			log.Println("HandleRoot > refreshToken:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	client.SetUserAccessToken(token.AccessToken)

	user, err := client.GetUsers(nil)
	if err != nil {
		log.Println("HandleRoot > client.GetUsers:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(user.Data.Users) == 0 {
		// TODO: dar logout...
		log.Println("HandleRoot > len(userData.Users):", len(user.Data.Users))
		return
	}
	log.Println("HandleRoot > channel:", user.Data.Users[0].ID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("index").Parse(loggedInHTML)
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

func refreshToken(w http.ResponseWriter, r *http.Request, client *helix.Client, refreshToken string) (*helix.AccessCredentials, error) {
	resp, err := client.RefreshUserAccessToken(refreshToken)
	if err != nil {
		return nil, err
	}
	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Printf("corrupted session %s -- generated new", err)
		err = nil
	}
	session.Values[oauthTokenKey] = resp.Data
	err = session.Save(r, w)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
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

// HandleWebsocket
// arquitetura chupinhada daqui: https://github.com/gorilla/websocket/tree/master/examples/chat
func HandleWebsocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
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

	// Connect to Cybervox API websocket
	var cybervoxWS *websocket.Conn
	if cybervoxWS, _, err = cybervox.Dial(voxClientID, voxClientSecret); err != nil {
		log.Println("HandleWebsocket: cybervox connect error:", err)
		return
	}

	client := &Client{
		id:         userID,
		hub:        hub,
		conn:       conn,
		cybervoxWS: cybervoxWS,
		send:       make(chan *Message, 256),
	}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func setupCORS(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func HandleTTS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	setupCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	log.Println("HandleTTS > URL:", r.URL)
	split := strings.Split(r.URL.Path, "/")
	if len(split) != 3 {
		log.Println("HandleWebsocket > len(split):", len(split))
		return
	}
	authHeader := ""
	tokenString := ""
	if authHeader = r.Header.Get("Authorization"); authHeader != "" {
		tokenString = strings.Split(authHeader, " ")[1]
	}
	if tokenString == "" {
		log.Println("HandleTTS > empty TokenString:", authHeader)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var token *jwt.Token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(""), nil
	})
	//if err != nil {
	//	log.Println("HandleTTS > error parsing jwt:", err)
	//	w.WriteHeader(http.StatusInternalServerError)
	//	return
	//}
	channelID := ""
	userID := ""
	if claims, ok := token.Claims.(jwt.MapClaims); ok { //&& token.Valid {
		channelID = claims["channel_id"].(string)
		userID = claims["user_id"].(string)
	} else {
		fmt.Println(err)
	}

	// verifica se canal está on
	if _, found := hub.clients[channelID]; !found {
		log.Println("HandleTTS > hub.clients not found:", channelID)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		log.Println("HandleRoot > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	accessToken, err := client.RequestAppAccessToken(nil)
	if err != nil {
		return
	}
	client.SetAppAccessToken(accessToken.Data.AccessToken)

	var userName, userPicture string
	users, err := client.GetUsers(&helix.UsersParams{IDs: []string{userID}})
	if err == nil {
		userName = users.Data.Users[0].DisplayName
		userPicture = users.Data.Users[0].ProfileImageURL
	}

	// vai na cybervox gerar o audio...
	text := r.FormValue("text")
	var url string
	if c, ok := hub.clients[channelID]; ok {
		var err error
		if url, err = c.TTS(text); err != nil {
			log.Println("HandleTTS > error calling cybervox:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	// manda url do audio para o websocket do canal
	hub.broadcast <- &Message{
		ClientID:    channelID,
		AudioURL:    url,
		Text:        text,
		UserName:    userName,
		UserPicture: userPicture,
	}
}

func main() {
	// Gob encoding for helix/AccessCredentials
	gob.Register(&helix.AccessCredentials{})

	// TODO: hub se conecta no cybervox...
	hub := newHub()
	go hub.run()

	mux := http.DefaultServeMux
	mux.HandleFunc("/login", HandleLogin)
	mux.HandleFunc("/logout", HandleLogout)
	mux.HandleFunc("/redirect", HandleOAuth2Callback)
	mux.HandleFunc("/layer/", HandleLayer)
	mux.HandleFunc("/ws/", func(w http.ResponseWriter, r *http.Request) {
		HandleWebsocket(hub, w, r)
	})
	mux.HandleFunc("/tts/", func(w http.ResponseWriter, r *http.Request) {
		HandleTTS(hub, w, r)
	})
	mux.HandleFunc("/elm.min.js", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(elmMinJs); err != nil {
			log.Println("main > elm.min.js:", err)
			return
		}
	})
	mux.HandleFunc("/", HandleRoot)

	fmt.Println("Started running on http://localhost:7001")
	fmt.Println(http.ListenAndServe(":7001", nil))
}
