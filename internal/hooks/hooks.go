package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/benatsnowyroad/arp-sidecar/internal/config"
)

// Payload is the body sent to the OpenClaw hooks endpoint.
type Payload struct {
	Message    string `json:"message"`
	SessionKey string `json:"sessionKey"`
	Name       string `json:"name,omitempty"`
	Deliver    bool   `json:"deliver"`
}

// Forwarder sends event payloads to the OpenClaw hooks endpoint.
type Forwarder struct {
	hooksURL   string
	hooksToken string
	client     *http.Client
	logger     *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) *Forwarder {
	return &Forwarder{
		hooksURL:   cfg.HooksURL,
		hooksToken: cfg.HooksToken,
		client:     &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// Send POSTs a payload to the hooks endpoint.
func (f *Forwarder) Send(ctx context.Context, p *Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.hooksURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.hooksToken)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending hook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("hook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	f.logger.Debug("hook delivered", "name", p.Name, "sessionKey", p.SessionKey, "status", resp.StatusCode)
	return nil
}
