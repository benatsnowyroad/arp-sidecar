package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/benatsnowyroad/arp-sidecar/internal/config"
	"github.com/benatsnowyroad/arp-sidecar/internal/hooks"
)

const (
	maxBackoff     = 60 * time.Second
	baseBackoff    = 1 * time.Second
	heartbeatGrace = 10 * time.Second
)

// Message represents an incoming ARP relay message.
type Message struct {
	Type      string          `json:"type"`
	MessageID string          `json:"messageId,omitempty"`
	ChannelID string          `json:"channelId,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`

	// Fields present on various message types
	Content   string `json:"content,omitempty"`
	SenderID  string `json:"senderId,omitempty"`
	TurnID    string `json:"turnId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// Client manages the WebSocket connection to the ARP relay.
type Client struct {
	cfg    *config.Config
	hooks  *hooks.Forwarder
	logger *slog.Logger

	mu             sync.Mutex
	lastSeenByChan map[string]string // channelId -> lastSeenMessageId
}

func New(cfg *config.Config, logger *slog.Logger) *Client {
	return &Client{
		cfg:            cfg,
		hooks:          hooks.New(cfg, logger),
		logger:         logger,
		lastSeenByChan: make(map[string]string),
	}
}

// Run connects to the relay and processes messages. It reconnects on failure.
func (c *Client) Run(ctx context.Context) error {
	attempt := 0

	for {
		connectedAt := time.Now()
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Reset backoff if we were connected for a while (successful connection).
		if time.Since(connectedAt) > 30*time.Second {
			attempt = 0
		}

		attempt++
		delay := backoff(attempt)
		c.logger.Warn("connection lost, reconnecting", "error", err, "attempt", attempt, "delay", delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	url := c.cfg.WebSocketURL()
	c.logger.Info("connecting", "url", redactToken(url))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, url, http.Header{})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial failed (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	c.logger.Info("connected to relay")

	// Reset backoff on successful connection (handled by caller resetting attempt on success)
	// We signal success by running the read loop.

	return c.readLoop(ctx, conn)
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.logger.Warn("invalid message", "error", err, "raw", string(raw))
			continue
		}

		c.handleMessage(ctx, conn, &msg)
	}
}

func (c *Client) handleMessage(ctx context.Context, conn *websocket.Conn, msg *Message) {
	switch msg.Type {
	case "hello":
		c.logger.Info("received hello from relay")
		// On reconnect, catch up missed messages for all tracked channels.
		c.catchUpAll(ctx)

	case "heartbeat":
		c.logger.Debug("heartbeat received, sending ack")
		ack := map[string]string{"type": "heartbeat_ack"}
		if err := conn.WriteJSON(ack); err != nil {
			c.logger.Warn("failed to send heartbeat ack", "error", err)
		}

	case "channel_message":
		c.trackMessage(msg)
		c.logger.Debug("channel_message buffered", "channelId", msg.ChannelID, "messageId", msg.MessageID)

	case "turn_notification":
		c.trackMessage(msg)
		c.logger.Info("turn_notification", "channelId", msg.ChannelID, "turnId", msg.TurnID)
		c.forwardToHooks(ctx, msg)

	case "synthesis_request":
		c.trackMessage(msg)
		c.logger.Info("synthesis_request", "channelId", msg.ChannelID)
		c.forwardToHooks(ctx, msg)

	case "mention_notification":
		c.trackMessage(msg)
		c.logger.Info("mention_notification", "channelId", msg.ChannelID)
		c.forwardToHooks(ctx, msg)

	default:
		c.logger.Debug("unhandled message type", "type", msg.Type)
	}
}

func (c *Client) trackMessage(msg *Message) {
	if msg.ChannelID == "" || msg.MessageID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeenByChan[msg.ChannelID] = msg.MessageID
}

func (c *Client) forwardToHooks(ctx context.Context, msg *Message) {
	sessionKey := fmt.Sprintf("arp:channel:%s", msg.ChannelID)

	payload := hooks.Payload{
		Type:       msg.Type,
		SessionKey: sessionKey,
		ChannelID:  msg.ChannelID,
		MessageID:  msg.MessageID,
		Content:    msg.Content,
		SenderID:   msg.SenderID,
		TurnID:     msg.TurnID,
		Data:       msg.Data,
	}

	if err := c.hooks.Send(ctx, &payload); err != nil {
		c.logger.Error("hook delivery failed", "type", msg.Type, "error", err)
	}
}

func (c *Client) catchUpAll(ctx context.Context) {
	c.mu.Lock()
	channels := make(map[string]string, len(c.lastSeenByChan))
	for ch, id := range c.lastSeenByChan {
		channels[ch] = id
	}
	c.mu.Unlock()

	for channelID, lastID := range channels {
		c.logger.Info("catching up channel", "channelId", channelID, "after", lastID)
		msgs, err := FetchMissed(ctx, c.cfg, channelID, lastID)
		if err != nil {
			c.logger.Error("catchup failed", "channelId", channelID, "error", err)
			continue
		}
		for _, msg := range msgs {
			c.trackMessage(&msg)
			if shouldForward(msg.Type) {
				c.forwardToHooks(ctx, &msg)
			}
		}
		c.logger.Info("catchup complete", "channelId", channelID, "fetched", len(msgs))
	}
}

func shouldForward(msgType string) bool {
	switch msgType {
	case "turn_notification", "synthesis_request", "mention_notification":
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt-1))
	d := time.Duration(exp) * baseBackoff

	if d > maxBackoff {
		d = maxBackoff
	}

	// Add jitter: ±25%
	jitter := time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
	return jitter
}

func redactToken(url string) string {
	if i := len(url) - 1; i > 0 {
		// Just show the structure, not the token
		for j := 0; j < len(url); j++ {
			if url[j] == '=' {
				return url[:j+1] + "***"
			}
		}
	}
	return url
}
