package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/moniquelive/vox-twitch/dashboard/cybervox"
	"github.com/nicklaw5/helix"
	"github.com/parnurzeal/gorequest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
var (
	//go:embed .oauth_client_id
	clientID string
	//go:embed .oauth_client_secret
	clientSecret string
)

var (
	//go:embed .vox_client_id
	voxClientID string
	//go:embed .vox_client_secret
	voxClientSecret string
)

var (
	//go:embed logged-out.html
	loggedOutHTML []byte
	//go:embed logged-in.html
	loggedInHTML string
	//go:embed layer.html
	layerHtml string
	//go:embed elm/elm.min.js
	elmMinJs []byte
)

const (
	oauthSessionName = "oauth-session"
	oauthTokenKey    = "oauth-token"
)

var (
	scopes = []string{"user:read:email"}
	/**
	redirectURL = "http://localhost:7001/redirect"
	/*/
	redirectURL = "https://vox-twitch.monique.dev/redirect"
	/**/
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

var (
	usersConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vox_twitch_connected_users_total",
		Help: "The total number of connected users",
	})
	ttsGenerated = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vox_twitch_generated_tts_total",
		Help: "The total number of tts messages spoken",
	}, []string{"channel_id"},
	)
)

// HandleRoot is a Handler that shows a login button. In production, if the frontend is served / generated
// by Go, it should use html/template to prevent XSS attacks.
func HandleRoot(hub *Hub, w http.ResponseWriter, r *http.Request) {
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
			// log error
			log.Println("HandleRoot > refreshToken:", err)
			// clear cookies
			session.Options.MaxAge = -1
			if err = session.Save(r, w); err != nil {
				log.Println("session.Save:", err)
			}
			// return to home page
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
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
		// log error
		log.Println("HandleRoot > len(userData.Users):", len(user.Data.Users))
		// clear cookies
		session.Options.MaxAge = -1
		if err = session.Save(r, w); err != nil {
			log.Println("session.Save:", err)
		}
		// return to home page
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	log.Println("HandleRoot > channel:", user.Data.Users[0].ID)
	////const botID = "661856691"
	////const profID = "551257512"
	////const punkID = "533882077"
	//information, err := client.GetStreams(&helix.StreamsParams{
	//	UserIDs: []string{user.Data.Users[0].ID},
	//})
	//fmt.Println("len:", len(information.Data.Streams)) // len == 1 ? LIVE : OFFLINE

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("index").Parse(loggedInHTML)
	if err != nil {
		log.Printf("HandleRoot > error creating template: %v", err)
		return
	}
	parsed := bytes.NewBufferString("")
	vars := struct {
		UserID string
		Online []TwitchUser
	}{
		UserID: user.Data.Users[0].ID,
		Online: hub.Online(client),
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

	// TODO: verificar se FormValue("state") é valido!

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

type betterTTV struct {
	UrlTemplate string   `json:"urlTemplate"`
	Bots        []string `json:"bots"`
	Emotes      []struct {
		Id        string `json:"id"`
		Channel   string `json:"channel"`
		Code      string `json:"code"`
		ImageType string `json:"imageType"`
	} `json:"emotes"`
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
		split := strings.Split(authHeader, " ")
		if len(split) == 2 {
			tokenString = split[1]
		}
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
		return base64.StdEncoding.DecodeString("gYPYgF/qbvWe+tp9bmhsXapRyXQATBQcVg1YVelr3Ss=")
	})
	channelID := ""
	userID := ""
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		channelID = claims["channel_id"].(string)
		userID = claims["user_id"].(string)
	} else {
		fmt.Println(err)
	}

	// verifica se canal está on
	if _, found := hub.clients[channelID]; !found {
		log.Println("HandleTTS > hub.clients not found:", channelID)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fmt.Sprintf("channel %v is not online", channelID)))
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
	var audioURL string
	if c, ok := hub.clients[channelID]; ok {
		var err error
		for {
			audioURL, err = c.TTS(text)
			if err == nil || !strings.Contains(err.Error(), "busy") {
				break
			}
			time.Sleep(time.Second)
		}
		if err != nil {
			log.Println("HandleTTS > error calling cybervox:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	var emotes map[string]string
	channel, err := client.GetChannelInformation(&helix.GetChannelInformationParams{BroadcasterID: channelID})
	if err == nil && len(channel.Data.Channels) == 1 {
		var betterTTV betterTTV
		_, _, errs := gorequest.New().
			Get("https://api.betterttv.net/2/channels/" + channel.Data.Channels[0].BroadcasterName).
			EndStruct(&betterTTV)
		if errs == nil {
			emotes = make(map[string]string, len(betterTTV.Emotes))
			for _, emote := range betterTTV.Emotes {
				emotes[emote.Code] = emote.Id
			}
		}
	}

	// manda url do audio para o websocket do canal
	hub.broadcast <- &Message{
		ClientID:    channelID,
		AudioURL:    audioURL,
		Text:        text,
		Emotes:      emotes,
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		HandleRoot(hub, w, r)
	})
	mux.HandleFunc("/login", HandleLogin)
	mux.HandleFunc("/redirect", HandleOAuth2Callback)

	mux.HandleFunc("/logout", HandleLogout)
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
	mux.Handle("/metrics", promhttp.Handler())

	fmt.Println("Started running on http://localhost:7001")
	fmt.Println(http.ListenAndServe(":7001", nil))
}
