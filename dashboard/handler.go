package main

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/nicklaw5/helix"
	"github.com/parnurzeal/gorequest"
	"github.com/streadway/amqp"
)

const (
	oauthSessionName = "oauth-session"
	oauthTokenKey    = "oauth-token"
)

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

var (
	scopes = []string{"user:read:email"}
	/**
	redirectURL = "http://localhost:7001/redirect"
	/*/
	redirectURL = "https://vox-twitch.monique.dev/redirect"
	/**/
	cookieSecret = []byte("my awesome cookie secret <3 monique.dev")
	cookieStore  = sessions.NewCookieStore(cookieSecret)

	//go:embed .oauth_client_id
	clientID string
	//go:embed .oauth_client_secret
	clientSecret string
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleRoot is a Handler that shows a login button. In production, if the frontend is served / generated
// by Go, it should use html/template to prevent XSS attacks.
func HandleRoot(hub *Hub, w http.ResponseWriter, r *http.Request) {
	log.Println("URL:", r.URL)
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// get cookie jar
	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Printf("HandleRoot > corrupted session: %v -- generated new", err)
	}

	// session still valid?
	oauthToken := session.Values[oauthTokenKey]
	if oauthToken == nil {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, err = w.Write(loggedOutHTML)
		return
	}

	// grab access token from cookies
	var ok bool
	token, ok := oauthToken.(*helix.AccessCredentials)
	if !ok {
		log.Println("HandleRoot > oauthToken is not *helix.AccessCredentials")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// new twitch api client
	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		log.Println("HandleRoot > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// access token still valid?
	if ok, _, _ := client.ValidateToken(token.AccessToken); !ok {
		// create new access token using the refresh token
		var resp *helix.RefreshTokenResponse
		if resp, err = client.RefreshUserAccessToken(token.RefreshToken); err != nil {
			log.Println("HandleRoot > refreshToken:", err)
			// clear cookies
			session.Options.MaxAge = -1
			if err = session.Save(r, w); err != nil {
				log.Println("session.Save:", err)
			}
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}
		session, err := cookieStore.Get(r, oauthSessionName)
		if err != nil {
			log.Printf("HandleRoot > corrupted session: %v -- generated new", err)
		}
		session.Values[oauthTokenKey] = resp.Data
		if session.Save(r, w) != nil {
			log.Println("HandleRoot > error saving session:", err)
			return
		}
		token = &resp.Data
	}

	// update access token if necessary
	client.SetUserAccessToken(token.AccessToken)

	// get current user profile
	user, err := client.GetUsers(nil)
	if err != nil {
		log.Println("HandleRoot > client.GetUsers:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// sanity check
	if len(user.Data.Users) == 0 {
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

	// current user
	log.Println("HandleRoot > channel:", user.Data.Users[0].ID)
	////const botID = "661856691"
	////const profID = "551257512"
	////const punkID = "533882077"
	//information, err := client.GetStreams(&helix.StreamsParams{
	//	UserIDs: []string{user.Data.Users[0].ID},
	//})
	//fmt.Println("len:", len(information.Data.Streams)) // len == 1 ? LIVE : OFFLINE

	// load login page template
	tmpl, err := template.New("index").Parse(loggedInHTML)
	if err != nil {
		log.Println("HandleRoot > error creating template:", err)
		return
	}

	// update login page template
	parsed := bytes.NewBufferString("")
	err = tmpl.Execute(parsed, struct {
		UserID string
		Online []TwitchUser
	}{UserID: user.Data.Users[0].ID, Online: hub.Online(client)})
	if err != nil {
		log.Println("HandleRoot > error parsing html:", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(parsed.Bytes())
}

// HandleLogin is a Handler that redirects the user to Twitch for login, and provides the 'state'
// parameter which protects against login CSRF.
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	// new twitch api client
	client, err := helix.NewClient(&helix.Options{
		ClientID:    clientID,
		RedirectURI: redirectURL,
	})
	if err != nil {
		log.Println("HandleLogin > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// create state token
	var tokenBytes [255]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		log.Println("HandleLogin > Couldn't generate a token:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// encode state token
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
	// get cookie jar
	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Println("HandleLogout > cookieStore.Get:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// clear cookies
	session.Options.MaxAge = -1
	if err = session.Save(r, w); err != nil {
		log.Println("HandleLogout > session.Save:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleOAuth2Callback is a Handler for oauth's 'redirect_uri' endpoint;
// it validates the state token and retrieves an OAuth token from the request parameters.
func HandleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	// new twitch api client
	client, err := helix.NewClient(&helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURL,
	})
	if err != nil {
		log.Println("HandleOAuth2Callback > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO: verificar se FormValue("state") é valido!

	code := r.FormValue("code")

	token, err := client.RequestUserAccessToken(code)
	if err != nil {
		log.Println("HandleOAuth2Callback > RequestUserAccessToken:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// get cookie jar
	session, err := cookieStore.Get(r, oauthSessionName)
	if err != nil {
		log.Printf("HandleOAuth2Callback > corrupted session: %v -- generated new", err)
	}

	// store cookie with access token data
	session.Values[oauthTokenKey] = token.Data
	if err = session.Save(r, w); err != nil {
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	return
}

// HandleLayer responds a personalized layer for the current user.
func HandleLayer(w http.ResponseWriter, r *http.Request) {
	log.Println("HandleLayer > URL:", r.URL)
	split := strings.Split(r.URL.Path, "/")
	if len(split) != 4 {
		log.Println("HandleLayer > len(split):", len(split))
		return
	}
	userID := split[2]
	log.Println("HandleLayer > userID:", userID)

	// load layer page template
	tmpl, err := template.New("layer").Parse(layerHtml)
	if err != nil {
		log.Println("HandleLayer > error creating template:", err)
		return
	}

	// update layer page template
	parsed := bytes.NewBufferString("")
	err = tmpl.Execute(parsed, struct {
		UserID string
	}{UserID: userID})
	if err != nil {
		log.Println("HandleLayer > error parsing html:", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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

	// create current user state
	client := &Client{
		id:   userID,
		hub:  hub,
		conn: conn,
		send: make(chan *Message, 256),
	}

	// Connect to voxfala RabbitMQ
	if client.amqpConn, err = amqp.Dial(os.Getenv("RABBITMQ_URL")); err != nil {
		log.Println("MQ connection:", err)
	}
	if client.amqpChan, err = client.amqpConn.Channel(); err != nil {
		log.Println("MQ channel:", err)
	}
	if err = client.amqpChan.Qos(1, 0, false); err != nil {
		log.Println("MQ channel QoS:", err)
	}

	// register current user state
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func HandleTTS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

	if r.Method == "OPTIONS" {
		return
	}

	log.Println("HandleTTS > URL:", r.URL)

	var split []string
	if split = strings.Split(r.URL.Path, "/"); len(split) != 3 {
		log.Println("HandleTTS > len(url split):", len(split))
		return
	}
	var authHeader string
	if authHeader = r.Header.Get("Authorization"); authHeader == "" {
		log.Println("HandleTTS > Authorization header not found")
		return
	}
	if split = strings.Split(authHeader, " "); len(split) != 2 {
		log.Println("HandleTTS > len(auth split):", len(split))
		return
	}
	// [0]="Bearer" [1]=token
	tokenString := split[1]
	if tokenString == "" {
		log.Println("HandleTTS > empty TokenString:", authHeader)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var token, err = jwt.Parse(tokenString, func(tkn *jwt.Token) (interface{}, error) {
		if _, ok := tkn.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", tkn.Header["alg"])
		}
		return base64.StdEncoding.DecodeString("gYPYgF/qbvWe+tp9bmhsXapRyXQATBQcVg1YVelr3Ss=")
	})

	if err != nil || !token.Valid {
		log.Println("HandleTTS > error parsing jwt:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	channelID := ""
	userID := ""
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		channelID = claims["channel_id"].(string)
		userID = claims["user_id"].(string)
	}

	// is channel registered (online)?
	if _, found := hub.clients[channelID]; !found {
		log.Println("HandleTTS > hub.clients not found:", channelID)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fmt.Sprintf("channel %q is offline", channelID)))
		return
	}

	// new twitch api client
	var client *helix.Client
	if client, err = helix.NewClient(&helix.Options{ClientID: clientID, ClientSecret: clientSecret}); err != nil {
		log.Println("HandleTTS > NewClient:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// get app access token
	var accessToken *helix.AppAccessTokenResponse
	if accessToken, err = client.RequestAppAccessToken(nil); err != nil {
		log.Println("HandleTTS > RequestAppAccessToken:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// update client's access token
	client.SetAppAccessToken(accessToken.Data.AccessToken)

	var userName, userPicture string
	users, err := client.GetUsers(&helix.UsersParams{IDs: []string{userID}})
	if err == nil && len(users.Data.Users) > 0 {
		userName = users.Data.Users[0].DisplayName
		userPicture = users.Data.Users[0].ProfileImageURL
	}

	var (
		c  *Client
		ok bool
	)
	if c, ok = hub.clients[channelID]; !ok {
		log.Printf("HandleTTS > channel %q is offline: %v", channelID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// generates audio
	text := r.FormValue("text")
	var audioURL string
	const RETRIES = 5
	for i := 0; i < RETRIES; i++ {
		audioURL, err = c.TTS(text)
		if err == nil || !strings.Contains(err.Error(), "busy") {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Println("HandleTTS > error generating audio:", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var emotes map[string]string
	if channel, err := client.GetChannelInformation(&helix.GetChannelInformationParams{BroadcasterID: channelID}); err == nil && len(channel.Data.Channels) == 1 {
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

	// send audio url to channel's websocket
	hub.broadcast <- &Message{
		ClientID:    channelID,
		AudioURL:    audioURL,
		Text:        text,
		Emotes:      emotes,
		UserName:    userName,
		UserPicture: userPicture,
	}
}

func HandleTTSPlay(redisConn *redis.Client, w http.ResponseWriter, r *http.Request) {
	audioID := strings.TrimPrefix(r.URL.Path, "/ttsPlay/")
	b := redisConn.Get(context.Background(), audioID)
	if b.Err() != nil {
		log.Printf("HandleTTSPlay > audio %q not found: %v", audioID, b.Err())
		http.NotFound(w, r)
		return
	}
	bb, err := b.Bytes()
	if err != nil {
		log.Println("HandleTTSPlay > error getting audio bytes:", err)
		http.Error(w, "error getting bytes", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "audio.wav", time.Time{}, bytes.NewReader(bb))
}
