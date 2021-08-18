// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"github.com/gorilla/websocket"
)

type Message struct {
	ClientID string `json:"client_id"`
	AudioURL string `json:"audio_url"`
	Text     string `json:"text"`
	UserName string `json:"username"`
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
		select {
		case client := <-h.register:
			h.clients[client.id] = client
		case client := <-h.unregister:
			if c, ok := h.clients[client.id]; ok {
				c.cybervoxWS.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				c.cybervoxWS.Close()
				delete(h.clients, client.id)
				close(client.send)
			}
		case message := <-h.broadcast:
			client := h.clients[message.ClientID]
			select {
			case client.send <- message:
			default:
				close(client.send)
				delete(h.clients, client.id)
			}
		}
	}
}
