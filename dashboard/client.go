// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/streadway/amqp"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	id string

	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan *Message

	// RabbitMQ connection and channel
	amqpMutex sync.Mutex
	amqpConn  *amqp.Connection
	amqpChan  *amqp.Channel
}

type ttsResponse struct {
	Success bool   `json:"success"`
	AudioID string `json:"audio_id"`
	Reason  string `json:"reason"`
}

func (c *Client) TTS(text string) (string, error) {
	c.amqpMutex.Lock()
	defer c.amqpMutex.Unlock()

	// start model response consumer
	consumerID := uuid.New().String()
	responseCh, err := c.amqpChan.Consume(
		"amq.rabbitmq.reply-to", // queue
		consumerID,              // consumer
		true,                    // auto-ack
		false,                   // exclusive
		false,                   // no-local
		false,                   // no-wait
		nil,                     // args
	)
	if err != nil {
		return "", fmt.Errorf("failed to register the response consumer: %w", err)
	}
	defer func() {
		if err := c.amqpChan.Cancel(consumerID, false); err != nil {
			log.Println("failed to cancel the response consumer:", err)
		}
	}()

	requestBody, _ := json.Marshal(struct {
		Action string `json:"action"`
		Text   string `json:"text"`
	}{
		Action: "tts",
		Text:   text,
	})
	// send tts request to MQ
	correlationID := uuid.New().String()
	err = c.amqpChan.Publish(
		"",            // exchange
		"ms.vox_fala", // routing key
		false,         // mandatory
		false,         // immediate
		amqp.Publishing{
			ContentType:   "text/plain",
			Body:          requestBody,
			DeliveryMode:  amqp.Persistent,
			Expiration:    "60000",
			CorrelationId: correlationID,
			ReplyTo:       "amq.rabbitmq.reply-to",
		})
	if err != nil {
		return "", fmt.Errorf("publish message: %w", err)
	}

	timeoutTimer := time.NewTimer(5 * time.Minute)
	defer timeoutTimer.Stop()
	var responseBody []byte
	select {
	case d := <-responseCh:
		// assert correlationID == d.CorrelationId
		responseBody = d.Body
		break
	case <-timeoutTimer.C:
		return "", errors.New("timeout")
	}
	var response *ttsResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return "", fmt.Errorf("json unmarshal: %w", err)
	}
	return "https://vox-twitch.monique.dev/ttsPlay/" + response.AudioID, nil
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		// impossibru!
		log.Fatalln("unexpected message received:", string(message))
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
