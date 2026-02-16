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

// HookResponse is the response from OpenClaw hooks endpoint.
type HookResponse struct {
	OK        bool   `json:"ok"`
	RunID     string `json:"runId"`
	SessionID string `json:"sessionId"`
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
		client:     &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// Send POSTs a payload to the hooks endpoint and returns the runId.
func (f *Forwarder) Send(ctx context.Context, p *Payload) (*HookResponse, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.hooksURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.hooksToken)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending hook: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var hookResp HookResponse
	if err := json.Unmarshal(respBody, &hookResp); err != nil {
		f.logger.Warn("could not parse hook response", "body", string(respBody), "error", err)
		// Still return success if we got 2xx
		return &HookResponse{OK: true}, nil
	}

	f.logger.Debug("hook delivered", "name", p.Name, "sessionKey", p.SessionKey, "status", resp.StatusCode, "runId", hookResp.RunID)
	return &hookResp, nil
}
