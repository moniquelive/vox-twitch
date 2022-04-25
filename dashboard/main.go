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
package main

import (
	_ "embed"
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/nicklaw5/helix"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Gob encoding for helix/AccessCredentials
	gob.Register(&helix.AccessCredentials{})

	hub := newHub()
	go hub.run()

	redisURL := os.Getenv("REDIS_URL")
	if !strings.HasSuffix(redisURL, ":6379") {
		redisURL = redisURL + ":6379"
	}
	redisConn := redis.NewClient(&redis.Options{Addr: redisURL})

	mux := http.DefaultServeMux
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		HandleRoot(hub, w, r)
	})
	mux.HandleFunc("/login", HandleLogin)
	mux.HandleFunc("/redirect", HandleOAuth2Callback)
	mux.HandleFunc("/logout", HandleLogout)
	mux.HandleFunc("/layer/", HandleLayer)
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/ws/", func(w http.ResponseWriter, r *http.Request) {
		HandleWebsocket(hub, w, r)
	})
	mux.HandleFunc("/tts/", func(w http.ResponseWriter, r *http.Request) {
		HandleTTS(hub, w, r)
	})
	mux.HandleFunc("/ttsPlay/", func(w http.ResponseWriter, r *http.Request) {
		HandleTTSPlay(redisConn, w, r)
	})
	mux.HandleFunc("/elm.min.js", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(elmMinJs); err != nil {
			log.Println("main > elm.min.js:", err)
			return
		}
	})

	fmt.Println("Started running on :7001")
	fmt.Println(http.ListenAndServe(":7001", nil))
}
