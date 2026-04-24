package ws

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
)

type Client struct {
	ID   string
	Conn *websocket.Conn

	mu   sync.Mutex
	send chan []byte
}

func NewClient(id string, conn *websocket.Conn) *Client {
	return &Client{
		ID:   id,
		Conn: conn,
		send: make(chan []byte, 256),
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.send:
	default:
	}
	close(c.send)
	_ = c.Conn.Close()
}

func (c *Client) Outbound() <-chan []byte {
	return c.send
}

func (c *Client) SendEnvelope(msgType contracts.WSMsgType, reqID *string, payload any) error {
	env := struct {
		Type      contracts.WSMsgType `json:"type"`
		RequestID *string             `json:"requestId,omitempty"`
		Payload   any                 `json:"payload"`
	}{
		Type:      msgType,
		RequestID: reqID,
		Payload:   payload,
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.send <- b
	return nil
}
