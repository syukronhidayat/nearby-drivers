package handlers

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/syukronhidayat/nearby-drivers/internal/contracts"
	"github.com/syukronhidayat/nearby-drivers/internal/ws"
)

type WSHandler struct {
	Hub      *ws.Hub
	nextID   atomic.Uint64
	upgrader websocket.Upgrader
}

func NewWSHandler(hub *ws.Hub) *WSHandler {
	return &WSHandler{
		Hub: hub,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // tighten later
		},
	}
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	connID := h.nextConnID()
	c := ws.NewClient(connID, conn)

	h.Hub.Register(c)

	go writeLoop(c)
	readLoop(c, h.Hub)

	h.Hub.Unregister(connID)
}

func writeLoop(c *ws.Client) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.Outbound():
			if !ok {
				return
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func readLoop(c *ws.Client, hub *ws.Hub) {
	defer func() { _ = c.Conn.Close() }()

	for {
		_, b, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}

		var env contracts.WSEnvelope
		if err := json.Unmarshal(b, &env); err != nil {
			_ = c.SendEnvelope(contracts.WSMsgError, nil, contracts.WSErrorPayload{
				Code:    contracts.HTTPErrorValidation,
				Message: "invalid envelope",
				Details: map[string]any{"error": err.Error()},
			})
			continue
		}

		switch env.Type {
		case contracts.WSMsgSubscribeLocationDrivers:
			var p contracts.WSSubscribeLocationDriverPayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				_ = c.SendEnvelope(contracts.WSMsgError, env.RequestID, contracts.WSErrorPayload{
					Code:    contracts.HTTPErrorValidation,
					Message: "invalid subscribe payload",
					Details: map[string]any{"error": err.Error()},
				})
				continue
			}
			hub.Subscribe(c.ID, env.RequestID, p)

		case contracts.WSMsgUnsubscribe:
			var p contracts.WSUnsubscribePayload
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				_ = c.SendEnvelope(contracts.WSMsgError, env.RequestID, contracts.WSErrorPayload{
					Code:    contracts.HTTPErrorValidation,
					Message: "invalid unsubscribe payload",
					Details: map[string]any{"error": err.Error()},
				})
				continue
			}
			hub.UnsubscribeByID(c.ID, env.RequestID, p.SubscriptionID)

		default:
			_ = c.SendEnvelope(contracts.WSMsgError, env.RequestID, contracts.WSErrorPayload{
				Code:    contracts.HTTPErrorValidation,
				Message: "unknown message type",
				Details: map[string]any{"type": string(env.Type)},
			})
		}
	}
}

func (h *WSHandler) nextConnID() string {
	n := h.nextID.Add(1)
	return "c" + itoa(n)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(b[i:])
}
