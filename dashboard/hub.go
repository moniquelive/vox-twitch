// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"

	"github.com/gorilla/websocket"
	"github.com/nicklaw5/helix"
	"github.com/prometheus/client_golang/prometheus"
)

type Message struct {
	ClientID    string            `json:"client_id"`
	AudioURL    string            `json:"audio_url"`
	Text        string            `json:"text"`
	Emotes      map[string]string `json:"emotes"`
	UserName    string            `json:"username"`
	UserPicture string            `json:"user_picture"`
}

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[string]*Client

	// Inbound messages from the clients.
	broadcast chan *Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan *Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[string]*Client),
	}
}

func (h *Hub) run() {
	for {
		usersConnected.Set(float64(len(h.clients)))
		select {
		case client := <-h.register:
			h.clients[client.id] = client
			h.printStatus()
		case client := <-h.unregister:
			if c, ok := h.clients[client.id]; ok {
				c.cybervoxWS.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				c.cybervoxWS.Close()
				delete(h.clients, client.id)
				close(client.send)
				h.printStatus()
			}
		case message := <-h.broadcast:
			client := h.clients[message.ClientID]
			select {
			case client.send <- message:
				ttsGenerated.With(prometheus.Labels{"channel_id": client.id}).Inc()
			default:
				close(client.send)
				delete(h.clients, client.id)
			}
		}
	}
}

func (h *Hub) printStatus() {
	clients := ""
	for c := range h.clients {
		clients += "\n" + c
	}
	log.Println("canais\n------------", clients)
}

type TwitchUser struct {
	//Id          string    `json:"_id"`
	//Type        string    `json:"type"`
	//Bio         string    `json:"bio"`
	//CreatedAt   time.Time `json:"created_at"`
	//UpdatedAt   time.Time `json:"updated_at"`
	DisplayName string `json:"display_name"`
	Name        string `json:"name"`
	Logo        string `json:"logo"`
}

func (h *Hub) Online(client *helix.Client) (online []TwitchUser) {
	ids := make([]string, 0, len(h.clients))
	for c := range h.clients {
		ids = append(ids, c)
	}
	resp, err := client.GetUsers(&helix.UsersParams{IDs: ids})
	if err != nil {
		return
	}
	for _, user := range resp.Data.Users {
		online = append(online, TwitchUser{
			DisplayName: user.DisplayName,
			Name:        user.Login,
			Logo:        user.ProfileImageURL,
		})
	}
	return
}
